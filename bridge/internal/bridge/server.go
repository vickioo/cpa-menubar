package bridge

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	cfg      Config
	client   *http.Client
	sessions map[string]*oauthSession
	mu       sync.Mutex
}

func NewServer(cfg Config) (*Server, error) {
	server := &Server{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.UsageTimeout,
		},
		sessions: make(map[string]*oauthSession),
	}
	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "cpa-desktop-bridge"})
		return
	}

	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/desktop/v1/summary":
		s.handleSummary(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/desktop/v1/accounts":
		s.handleAccounts(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/desktop/v1/oauth/codex/start":
		s.handleOAuthStart(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/desktop/v1/oauth/codex/callback":
		s.handleOAuthCallback(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/desktop/v1/oauth/status":
		s.handleOAuthStatus(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/desktop/v1/accounts/") && strings.HasSuffix(r.URL.Path, "/reset-credit"):
		s.handleResetCredit(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) authorized(r *http.Request) bool {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) < 7 || !strings.EqualFold(header[:7], "Bearer ") {
		return false
	}
	provided := strings.TrimSpace(header[7:])
	if len(provided) != len(s.cfg.DesktopToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.DesktopToken)) == 1
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func (s *Server) pruneSessions(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for state, session := range s.sessions {
		if now.After(session.ExpiresAt.Add(5 * time.Minute)) {
			delete(s.sessions, state)
		}
	}
}
