package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type authFile struct {
	FileName     string
	Provider     string
	Email        string
	Plan         string
	AccountID    string
	AccessToken  string
	RefreshToken string
	IDToken      string
	Disabled     bool
	Expired      string
	LastRefresh  string
	IsK12        bool
	Claims       jwtClaims
}

type jwtClaims struct {
	Email string `json:"email"`
	Exp   int64  `json:"exp"`
	Auth  struct {
		ChatGPTAccountID               string `json:"chatgpt_account_id"`
		ChatGPTPlanType                string `json:"chatgpt_plan_type"`
		ChatGPTSubscriptionActiveUntil any    `json:"chatgpt_subscription_active_until"`
	} `json:"https://api.openai.com/auth"`
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AccountSource == "sub2" {
		accounts, err := s.loadSub2Accounts(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read Sub2 account status")
			return
		}
		summary := Summary{Source: "sub2", GeneratedAt: time.Now(), AccountsTotal: len(accounts), CodexAccounts: len(accounts), Services: map[string]bool{"sub2": s.db != nil}}
		for _, account := range accounts {
			if account.Valid { summary.ValidAccounts++ }
			if account.Focus { summary.FocusAccounts++ }
			if account.ExpiredAt != "" { summary.Expired++ }
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}
	files, err := s.loadAuthFiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read account status")
		return
	}
	now := time.Now()
	summary := Summary{
		GeneratedAt:   now,
		AccountsTotal: len(files),
		Services: map[string]bool{
			"cliproxy": s.tcpHealthy(s.cfg.CLIProxyURL),
			"router":   s.tcpHealthy(s.cfg.RouterURL),
		},
	}
	for _, file := range files {
		switch file.Provider {
		case "codex":
			summary.CodexAccounts++
		case "xai":
			summary.XAIAccounts++
		}
		if file.IsK12 {
			summary.K12Accounts++
		}
		if file.RefreshToken != "" {
			summary.Refreshable++
		}
		if expired(file.Expired, file.Claims.Exp, now) {
			summary.Expired++
		}
	}
	if summary.XAIAccounts > 0 || s.hasXAIOfficialConfig() {
		usage, status, usageErr := s.fetchXAIUsage(r.Context())
		summary.XAIUsage = usage
		summary.XAIUsageStatus = status
		if usageErr != nil {
			summary.XAIUsageError = publicRouterUsageError(usageErr)
		}
	} else {
		summary.XAIUsageStatus = "not_applicable"
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AccountSource == "sub2" {
		accounts, err := s.loadSub2Accounts(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read Sub2 accounts")
			return
		}
		writeJSON(w, http.StatusOK, AccountsResponse{GeneratedAt: time.Now(), Accounts: accounts})
		return
	}
	files, err := s.loadAuthFiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read accounts")
		return
	}

	accounts := make([]Account, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for index := range files {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			file := files[index]
			account := accountFromAuth(file)
			if file.Provider == "codex" && !file.Disabled && file.AccessToken != "" {
				sem <- struct{}{}
				usage, status, usageErr := s.fetchUsage(r.Context(), file)
				<-sem
				account.Usage = usage
				account.UsageStatus = status
				if usageErr != nil {
					account.UsageError = publicUsageError(usageErr)
				}
			}
			accounts[index] = account
		}()
	}
	wg.Wait()

	sort.SliceStable(accounts, func(i, j int) bool {
		if accounts[i].Provider != accounts[j].Provider {
			return accounts[i].Provider < accounts[j].Provider
		}
		return accounts[i].EmailMasked < accounts[j].EmailMasked
	})
	writeJSON(w, http.StatusOK, AccountsResponse{GeneratedAt: time.Now(), Accounts: accounts})
}

func (s *Server) loadAuthFiles() ([]authFile, error) {
	entries, err := os.ReadDir(s.cfg.AuthDir)
	if err != nil {
		return nil, err
	}
	files := make([]authFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(s.cfg.AuthDir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var raw map[string]any
		if json.Unmarshal(data, &raw) != nil {
			continue
		}
		provider := stringValue(raw["type"])
		if provider == "" {
			provider = stringValue(raw["provider"])
		}
		file := authFile{
			FileName:     entry.Name(),
			Provider:     provider,
			Email:        stringValue(raw["email"]),
			AccountID:    stringValue(raw["account_id"]),
			AccessToken:  stringValue(raw["access_token"]),
			RefreshToken: stringValue(raw["refresh_token"]),
			IDToken:      stringValue(raw["id_token"]),
			Disabled:     boolValue(raw["disabled"]),
			Expired:      stringValue(raw["expired"]),
			LastRefresh:  stringValue(raw["last_refresh"]),
			Plan:         stringValue(raw["plan"]),
			IsK12:        strings.Contains(strings.ToLower(entry.Name()), "k12"),
		}
		if claims, claimsErr := parseJWTClaims(file.IDToken); claimsErr == nil {
			file.Claims = claims
			if file.Email == "" {
				file.Email = claims.Email
			}
			if file.AccountID == "" {
				file.AccountID = claims.Auth.ChatGPTAccountID
			}
			file.Plan = claims.Auth.ChatGPTPlanType
		}
		if strings.Contains(strings.ToLower(file.Plan), "k12") {
			file.IsK12 = true
		}
		files = append(files, file)
	}
	return files, nil
}

func accountFromAuth(file authFile) Account {
	return Account{
		ID:                accountPublicID(file.FileName),
		Provider:          file.Provider,
		EmailMasked:       maskEmail(file.Email),
		Plan:              file.Plan,
		Disabled:          file.Disabled,
		ExpiredAt:         file.Expired,
		LastRefreshAt:     file.LastRefresh,
		HasRefreshToken:   file.RefreshToken != "",
		IsK12:             file.IsK12,
		UsageStatus:       "not_applicable",
		SubscriptionUntil: formatClaimValue(file.Claims.Auth.ChatGPTSubscriptionActiveUntil),
	}
}

func (s *Server) fetchUsage(ctx context.Context, file authFile) (json.RawMessage, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return nil, "error", err
	}
	request.Header.Set("Authorization", "Bearer "+file.AccessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal")
	if file.AccountID != "" {
		request.Header.Set("Chatgpt-Account-Id", file.AccountID)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, "error", err
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body, 512*1024)
	if err != nil {
		return nil, "error", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Sprintf("http_%d", response.StatusCode), fmt.Errorf("usage request returned %d", response.StatusCode)
	}
	sanitized, err := sanitizeUsage(data)
	if err != nil {
		return nil, "error", err
	}
	return sanitized, "ok", nil
}

func sanitizeUsage(data []byte) (json.RawMessage, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	allowed := make(map[string]any)
	for _, key := range []string{"plan_type", "rate_limit", "rate_limit_reset_credits", "usage", "limits", "message_cap", "credits"} {
		if value, ok := raw[key]; ok {
			allowed[key] = sanitizeUsageValue(value)
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("usage response did not contain supported fields")
	}
	return json.Marshal(allowed)
}

func accountPublicID(fileName string) string {
	digest := sha256.Sum256([]byte(fileName))
	return hex.EncodeToString(digest[:])[:12]
}

func sanitizeUsageValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, nested := range typed {
			lowerKey := strings.ToLower(key)
			if strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "email") || strings.Contains(lowerKey, "account_id") {
				continue
			}
			clean[key] = sanitizeUsageValue(nested)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, nested := range typed {
			clean[index] = sanitizeUsageValue(nested)
		}
		return clean
	default:
		return value
	}
}

func parseJWTClaims(token string) (jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, errors.New("invalid JWT")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, err
	}
	var claims jwtClaims
	if err = json.Unmarshal(data, &claims); err != nil {
		return jwtClaims{}, err
	}
	return claims, nil
}

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "unknown"
	}
	local := email[:at]
	visible := 2
	if len(local) < visible {
		visible = len(local)
	}
	return local[:visible] + "***" + email[at:]
}

func expired(expiredAt string, jwtExp int64, now time.Time) bool {
	if value := strings.TrimSpace(expiredAt); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.Before(now)
		}
	}
	return jwtExp > 0 && time.Unix(jwtExp, 0).Before(now)
}

func stringValue(value any) string {
	if stringValue, ok := value.(string); ok {
		return strings.TrimSpace(stringValue)
	}
	return ""
}

func boolValue(value any) bool {
	boolValue, _ := value.(bool)
	return boolValue
}

func formatClaimValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed > 0 {
			return time.Unix(int64(typed), 0).Format(time.RFC3339)
		}
	}
	return ""
}

func publicUsageError(err error) string {
	message := err.Error()
	if strings.Contains(message, "401") || strings.Contains(message, "403") {
		return "authorization unavailable"
	}
	if strings.Contains(message, "429") {
		return "quota or rate limit reached"
	}
	return "usage unavailable"
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response too large")
	}
	return data, nil
}

func (s *Server) tcpHealthy(rawURL string) bool {
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	host := request.URL.Host
	if !strings.Contains(host, ":") {
		if request.URL.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	connection, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}
