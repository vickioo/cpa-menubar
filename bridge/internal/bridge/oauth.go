package bridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	codexAuthURL     = "https://auth.openai.com/oauth/authorize"
	codexTokenURL    = "https://auth.openai.com/oauth/token"
	codexClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexRedirectURI = "http://localhost:1455/auth/callback"
)

type oauthSession struct {
	State         string
	Verifier      string
	ExpiresAt     time.Time
	Status        string
	Message       string
	AccountMasked string
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (s *Server) handleOAuthStart(w http.ResponseWriter, _ *http.Request) {
	s.pruneSessions(time.Now())
	state, err := randomURLString(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start OAuth")
		return
	}
	verifier, err := randomURLString(64)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start OAuth")
		return
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	expiresAt := time.Now().Add(s.cfg.OAuthTimeout)

	params := url.Values{
		"client_id":                  {codexClientID},
		"response_type":              {"code"},
		"redirect_uri":               {codexRedirectURI},
		"scope":                      {"openid email profile offline_access"},
		"state":                      {state},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"prompt":                     {"login"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
	}

	s.mu.Lock()
	s.sessions[state] = &oauthSession{State: state, Verifier: verifier, ExpiresAt: expiresAt, Status: "pending"}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, OAuthStartResponse{
		State:       state,
		URL:         codexAuthURL + "?" + params.Encode(),
		CallbackURL: codexRedirectURI,
		ExpiresAt:   expiresAt,
	})
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	var request OAuthCallbackRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid callback payload")
		return
	}
	if request.RedirectURL != "" {
		parsed, err := url.Parse(request.RedirectURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid redirect URL")
			return
		}
		request.State = parsed.Query().Get("state")
		request.Code = parsed.Query().Get("code")
		if oauthErr := parsed.Query().Get("error"); oauthErr != "" {
			s.setSessionError(request.State, "authorization was declined")
			writeError(w, http.StatusBadRequest, "authorization was declined")
			return
		}
	}
	request.State = strings.TrimSpace(request.State)
	request.Code = strings.TrimSpace(request.Code)
	if request.State == "" || request.Code == "" {
		writeError(w, http.StatusBadRequest, "state and code are required")
		return
	}

	s.mu.Lock()
	session, ok := s.sessions[request.State]
	if !ok || time.Now().After(session.ExpiresAt) || session.Status != "pending" {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "unknown or expired OAuth session")
		return
	}
	verifier := session.Verifier
	session.Status = "exchanging"
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	accountMasked, err := s.exchangeAndSave(ctx, request.Code, verifier)
	if err != nil {
		s.setSessionError(request.State, "token exchange failed")
		writeError(w, http.StatusBadGateway, "token exchange failed")
		return
	}

	s.mu.Lock()
	if current := s.sessions[request.State]; current != nil {
		current.Status = "complete"
		current.AccountMasked = accountMasked
		current.Verifier = ""
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, OAuthStatusResponse{State: request.State, Status: "complete", AccountMasked: accountMasked})
}

func (s *Server) handleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		writeError(w, http.StatusBadRequest, "state is required")
		return
	}
	s.mu.Lock()
	session, ok := s.sessions[state]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "OAuth session not found")
		return
	}
	response := OAuthStatusResponse{State: state, Status: session.Status, Message: session.Message, AccountMasked: session.AccountMasked}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) exchangeAndSave(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {codexClientID},
		"code":          {code},
		"redirect_uri":  {codexRedirectURI},
		"code_verifier": {verifier},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body, 256*1024)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", response.StatusCode)
	}
	var tokens tokenResponse
	if err = json.Unmarshal(data, &tokens); err != nil {
		return "", err
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.IDToken == "" {
		return "", errors.New("token response missing required fields")
	}
	claims, err := parseJWTClaims(tokens.IDToken)
	if err != nil {
		return "", err
	}
	email := strings.TrimSpace(claims.Email)
	accountID := strings.TrimSpace(claims.Auth.ChatGPTAccountID)
	if email == "" || accountID == "" {
		return "", errors.New("identity token missing account metadata")
	}
	now := time.Now()
	storage := map[string]any{
		"id_token":      tokens.IDToken,
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"account_id":    accountID,
		"last_refresh":  now.Format(time.RFC3339),
		"email":         email,
		"type":          "codex",
		"expired":       now.Add(time.Duration(tokens.ExpiresIn) * time.Second).Format(time.RFC3339),
		"disabled":      false,
	}
	path := s.authPathForAccount(accountID, email, claims.Auth.ChatGPTPlanType)
	if existing, readErr := os.ReadFile(path); readErr == nil {
		var previous map[string]any
		if json.Unmarshal(existing, &previous) == nil {
			for _, key := range []string{"priority", "excluded-models", "prefix", "proxy_url"} {
				if value, exists := previous[key]; exists {
					storage[key] = value
				}
			}
		}
	}
	if err = writeJSONAtomic(path, storage, 0o600); err != nil {
		return "", err
	}
	return maskEmail(email), nil
}

func (s *Server) authPathForAccount(accountID, email, plan string) string {
	digest := sha256.Sum256([]byte(accountID))
	accountHash := hex.EncodeToString(digest[:])[:8]
	plan = normalizeFilePart(plan)
	email = normalizeFilePart(email)
	name := "codex-" + accountHash + "-" + email
	if plan != "" {
		name += "-" + plan
	}
	name += ".json"
	for _, file := range existingAuthFiles(s.cfg.AuthDir) {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var raw map[string]any
		if json.Unmarshal(data, &raw) == nil && stringValue(raw["account_id"]) == accountID {
			return file
		}
	}
	return filepath.Join(s.cfg.AuthDir, name)
}

func existingAuthFiles(dir string) []string {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	return files
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".cpa-auth-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(value); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func randomURLString(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func normalizeFilePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	re := regexp.MustCompile(`[^a-z0-9@._+-]+`)
	value = strings.Trim(re.ReplaceAllString(value, "-"), "-")
	if len(value) > 100 {
		value = value[:100]
	}
	return value
}

func (s *Server) setSessionError(state, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.sessions[state]; session != nil {
		session.Status = "error"
		session.Message = message
		session.Verifier = ""
	}
}
