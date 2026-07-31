package bridge

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sub2ProbeUsageURL        = "https://chatgpt.com/backend-api/wham/usage"
	sub2ProbeResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
)

const sub2ProbeAccountsQuery = `
SELECT account_id, account_type, platform, status, schedulable,
       expires_at, temp_unschedulable_until,
       access_token, chatgpt_account_id, automatic_expiry_at,
       proxy_protocol, proxy_host, proxy_port, proxy_username, proxy_password
FROM cpa_desktop_account_probe
ORDER BY account_id`

type sub2ProbeAccount struct {
	id                                int64
	accountType, platform, status     string
	schedulable                       bool
	expiresAt, tempUnschedulableUntil sql.NullTime
	accessToken, chatGPTAccountID     sql.NullString
	automaticExpiryAt                 sql.NullString
	proxyProtocol, proxyHost          sql.NullString
	proxyPort                         sql.NullInt64
	proxyUsername, proxyPassword      sql.NullString
}

type sub2ProbeSnapshot struct {
	status             string
	checkedAt          time.Time
	authorizationAlive *bool
	fiveHourUsed       *float64
	fiveHourResetAt    string
	weeklyUsed         *float64
	weeklyResetAt      string
	resetCredits       *int
	resetCreditExpiry  string
}

type sub2ProbeWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type sub2ProbeRateLimit struct {
	PrimaryWindow   *sub2ProbeWindow `json:"primary_window"`
	SecondaryWindow *sub2ProbeWindow `json:"secondary_window"`
}

type sub2ProbeResetCredits struct {
	AvailableCount int                     `json:"available_count"`
	Credits        []sub2ProbeCreditDetail `json:"credits"`
}

type sub2ProbeUsage struct {
	RateLimit             *sub2ProbeRateLimit    `json:"rate_limit"`
	RateLimitResetCredits *sub2ProbeResetCredits `json:"rate_limit_reset_credits"`
}

type sub2ProbeCreditDetail struct {
	ExpiresAt      string `json:"expires_at"`
	ExpiresAtCamel string `json:"expiresAt"`
	ResetType      string `json:"reset_type"`
	ResetTypeCamel string `json:"resetType"`
	Status         string `json:"status"`
}

func (s *Server) handleSub2Refresh(w http.ResponseWriter, r *http.Request) {
	if s.probeDB == nil {
		writeError(w, http.StatusServiceUnavailable, "Sub2 live probe is not configured")
		return
	}

	s.probeRunMu.Lock()
	defer s.probeRunMu.Unlock()

	startedAt := time.Now()
	accounts, err := s.loadSub2ProbeAccounts(r.Context(), startedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Sub2 probe accounts")
		return
	}

	concurrency := s.cfg.Sub2ProbeConcurrency
	if concurrency < 1 {
		concurrency = 3
	}
	sem := make(chan struct{}, concurrency)
	type outcome struct {
		snapshot sub2ProbeSnapshot
		skipped  bool
	}
	results := make(chan outcome, len(accounts))
	var wg sync.WaitGroup
	for _, account := range accounts {
		account := account
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			snapshot, skipped := s.refreshSub2ProbeAccount(r.Context(), account)
			<-sem
			results <- outcome{snapshot: snapshot, skipped: skipped}
		}()
	}
	wg.Wait()
	close(results)

	response := RefreshResponse{StartedAt: startedAt, CompletedAt: time.Now()}
	for result := range results {
		if result.skipped {
			response.SkippedByCooldown++
			continue
		}
		response.Attempted++
		switch result.snapshot.status {
		case "ok":
			response.Succeeded++
			if result.snapshot.resetCredits != nil {
				response.ResetCreditsRead++
			}
		case "unauthorized":
			response.Unauthorized++
		default:
			response.Failed++
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) loadSub2ProbeAccounts(ctx context.Context, now time.Time) ([]sub2ProbeAccount, error) {
	rows, err := s.probeDB.QueryContext(ctx, sub2ProbeAccountsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	accounts := make([]sub2ProbeAccount, 0)
	for rows.Next() {
		var account sub2ProbeAccount
		if err := rows.Scan(
			&account.id, &account.accountType, &account.platform, &account.status, &account.schedulable,
			&account.expiresAt, &account.tempUnschedulableUntil,
			&account.accessToken, &account.chatGPTAccountID, &account.automaticExpiryAt,
			&account.proxyProtocol, &account.proxyHost, &account.proxyPort,
			&account.proxyUsername, &account.proxyPassword,
		); err != nil {
			return nil, err
		}
		if account.platform != "openai" || account.accountType != "oauth" || account.status != "active" || !account.schedulable {
			continue
		}
		if account.expiresAt.Valid && !account.expiresAt.Time.After(now) {
			continue
		}
		if account.tempUnschedulableUntil.Valid && account.tempUnschedulableUntil.Time.After(now) {
			continue
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Server) refreshSub2ProbeAccount(ctx context.Context, account sub2ProbeAccount) (sub2ProbeSnapshot, bool) {
	now := time.Now()
	cooldown := s.cfg.Sub2ProbeCooldown
	if cooldown < 30*time.Second {
		cooldown = 30 * time.Second
	}

	s.probeMu.Lock()
	if last, ok := s.probeLastAttempt[account.id]; ok && now.Sub(last) < cooldown {
		snapshot := s.probeCache[account.id]
		s.probeMu.Unlock()
		return snapshot, true
	}
	s.probeLastAttempt[account.id] = now
	s.probeMu.Unlock()

	snapshot := s.fetchSub2Probe(ctx, account, now)
	s.probeMu.Lock()
	s.probeCache[account.id] = snapshot
	s.probeMu.Unlock()
	return snapshot, false
}

func (s *Server) fetchSub2Probe(ctx context.Context, account sub2ProbeAccount, checkedAt time.Time) sub2ProbeSnapshot {
	snapshot := sub2ProbeSnapshot{status: "error", checkedAt: checkedAt}
	accessToken := strings.TrimSpace(account.accessToken.String)
	chatGPTAccountID := strings.TrimSpace(account.chatGPTAccountID.String)
	if accessToken == "" || chatGPTAccountID == "" {
		snapshot.status = "unavailable"
		return snapshot
	}

	client, err := s.sub2ProbeHTTPClient(account)
	if err != nil {
		return snapshot
	}
	usageBody, statusCode, err := fetchSub2ProbeURL(ctx, client, sub2ProbeUsageURL, accessToken, chatGPTAccountID)
	if err != nil {
		return snapshot
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		alive := false
		snapshot.status = "unauthorized"
		snapshot.authorizationAlive = &alive
		return snapshot
	}
	if statusCode < 200 || statusCode >= 300 {
		return snapshot
	}

	var usage sub2ProbeUsage
	if err := json.Unmarshal(usageBody, &usage); err != nil {
		return snapshot
	}
	alive := true
	snapshot.status = "ok"
	snapshot.authorizationAlive = &alive
	applySub2Usage(&snapshot, usage, checkedAt)

	creditsBody, creditsStatus, creditsErr := fetchSub2ProbeURL(ctx, client, sub2ProbeResetCreditsURL, accessToken, chatGPTAccountID)
	if creditsErr == nil && creditsStatus >= 200 && creditsStatus < 300 {
		if count, expiry, present, parseErr := parseSub2ResetCreditDetails(creditsBody); parseErr == nil && present {
			snapshot.resetCredits = count
			snapshot.resetCreditExpiry = expiry
		}
	}
	return snapshot
}

func (s *Server) sub2ProbeHTTPClient(account sub2ProbeAccount) (*http.Client, error) {
	if !account.proxyHost.Valid || strings.TrimSpace(account.proxyHost.String) == "" {
		return s.client, nil
	}
	scheme := strings.ToLower(strings.TrimSpace(account.proxyProtocol.String))
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported probe proxy protocol")
	}
	proxyURL := &url.URL{Scheme: scheme, Host: net.JoinHostPort(account.proxyHost.String, strconv.FormatInt(account.proxyPort.Int64, 10))}
	if account.proxyUsername.Valid && strings.TrimSpace(account.proxyUsername.String) != "" {
		proxyURL.User = url.UserPassword(account.proxyUsername.String, account.proxyPassword.String)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	return &http.Client{Transport: transport, Timeout: s.cfg.UsageTimeout}, nil
}

func fetchSub2ProbeURL(ctx context.Context, client *http.Client, endpoint, accessToken, chatGPTAccountID string) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Chatgpt-Account-Id", chatGPTAccountID)
	request.Header.Set("Openai-Beta", "codex-1")
	request.Header.Set("Oai-Language", "zh-CN")
	request.Header.Set("Originator", "Codex Desktop")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Sec-Fetch-Site", "none")
	request.Header.Set("Sec-Fetch-Mode", "no-cors")
	request.Header.Set("Sec-Fetch-Dest", "empty")
	request.Header.Set("Priority", "u=4, i")
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := readLimited(response.Body, 512*1024)
	return body, response.StatusCode, err
}

func applySub2Usage(snapshot *sub2ProbeSnapshot, usage sub2ProbeUsage, checkedAt time.Time) {
	if usage.RateLimit != nil {
		windows := []*sub2ProbeWindow{usage.RateLimit.PrimaryWindow, usage.RateLimit.SecondaryWindow}
		for _, window := range windows {
			if window == nil {
				continue
			}
			used := window.UsedPercent
			resetAt := sub2WindowResetAt(window, checkedAt)
			if window.LimitWindowSeconds >= 24*60*60 {
				snapshot.weeklyUsed = &used
				snapshot.weeklyResetAt = resetAt
			} else {
				snapshot.fiveHourUsed = &used
				snapshot.fiveHourResetAt = resetAt
			}
		}
	}
	if usage.RateLimitResetCredits != nil {
		count := usage.RateLimitResetCredits.AvailableCount
		snapshot.resetCredits = &count
		snapshot.resetCreditExpiry = earliestSub2CreditExpiry(usage.RateLimitResetCredits.Credits)
	}
}

func sub2WindowResetAt(window *sub2ProbeWindow, checkedAt time.Time) string {
	if window.ResetAt > 0 {
		return time.Unix(window.ResetAt, 0).UTC().Format(time.RFC3339)
	}
	if window.ResetAfterSeconds > 0 {
		return checkedAt.Add(time.Duration(window.ResetAfterSeconds) * time.Second).UTC().Format(time.RFC3339)
	}
	return ""
}

func parseSub2ResetCreditDetails(body []byte) (*int, string, bool, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, "", false, nil
	}
	var credits []sub2ProbeCreditDetail
	var count *int
	present := false
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &credits); err != nil {
			return nil, "", false, err
		}
		present = true
	} else {
		var payload struct {
			AvailableCount      json.RawMessage `json:"available_count"`
			AvailableCountCamel json.RawMessage `json:"availableCount"`
			Credits             json.RawMessage `json:"credits"`
			ResetCredits        json.RawMessage `json:"rate_limit_reset_credits"`
			Items               json.RawMessage `json:"items"`
			Data                json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return nil, "", false, err
		}
		count = parseSub2ResetCount(payload.AvailableCount, payload.AvailableCountCamel)
		for _, raw := range []json.RawMessage{payload.Credits, payload.ResetCredits, payload.Items, payload.Data} {
			raw = bytes.TrimSpace(raw)
			if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
				continue
			}
			if err := json.Unmarshal(raw, &credits); err != nil {
				return count, "", count != nil, err
			}
			present = true
			break
		}
	}

	available := make([]sub2ProbeCreditDetail, 0, len(credits))
	for _, credit := range credits {
		resetType := strings.TrimSpace(credit.ResetType)
		if resetType == "" {
			resetType = strings.TrimSpace(credit.ResetTypeCamel)
		}
		if resetType != "" && !strings.EqualFold(resetType, "codex_rate_limits") {
			continue
		}
		if status := strings.TrimSpace(credit.Status); status != "" && !strings.EqualFold(status, "available") {
			continue
		}
		available = append(available, credit)
	}
	if count == nil && present {
		value := len(available)
		count = &value
	}
	return count, earliestSub2CreditExpiry(available), present || count != nil, nil
}

func parseSub2ResetCount(values ...json.RawMessage) *int {
	for _, value := range values {
		trimmed := bytes.TrimSpace(value)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		var parsed int
		if trimmed[0] == '"' {
			var text string
			if json.Unmarshal(trimmed, &text) != nil {
				continue
			}
			value, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil {
				continue
			}
			parsed = value
		} else if json.Unmarshal(trimmed, &parsed) != nil {
			continue
		}
		if parsed >= 0 {
			return &parsed
		}
	}
	return nil
}

func earliestSub2CreditExpiry(credits []sub2ProbeCreditDetail) string {
	var earliest time.Time
	var fallback string
	for _, credit := range credits {
		value := strings.TrimSpace(credit.ExpiresAt)
		if value == "" {
			value = strings.TrimSpace(credit.ExpiresAtCamel)
		}
		if value == "" {
			continue
		}
		if fallback == "" {
			fallback = value
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err == nil && (earliest.IsZero() || parsed.Before(earliest)) {
			earliest = parsed
		}
	}
	if !earliest.IsZero() {
		return earliest.UTC().Format(time.RFC3339)
	}
	return fallback
}

func (s *Server) applySub2Probe(account *Account, internalID int64) {
	s.probeMu.RLock()
	snapshot, ok := s.probeCache[internalID]
	s.probeMu.RUnlock()
	if !ok {
		if account.AuthorizationType == "apikey" {
			account.AuthorizationProbeStatus = "not_applicable"
		} else {
			account.AuthorizationProbeStatus = "not_checked"
		}
		return
	}
	account.AuthorizationProbeStatus = snapshot.status
	account.AuthorizationAlive = snapshot.authorizationAlive
	if !snapshot.checkedAt.IsZero() {
		account.AuthorizationCheckedAt = snapshot.checkedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.status != "ok" {
		return
	}
	if snapshot.fiveHourUsed != nil {
		account.FiveHourUsed = snapshot.fiveHourUsed
		account.FiveHourResetAt = snapshot.fiveHourResetAt
	}
	if snapshot.weeklyUsed != nil {
		account.WeeklyUsed = snapshot.weeklyUsed
		account.WeeklyResetAt = snapshot.weeklyResetAt
	}
	account.ResetCredits = snapshot.resetCredits
	account.ResetCreditExpiry = snapshot.resetCreditExpiry
	account.UsageUpdatedAt = snapshot.checkedAt.UTC().Format(time.RFC3339)
}

func normalizeSub2Expiry(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	if number, err := strconv.ParseInt(value, 10, 64); err == nil && number > 0 {
		if number > 10_000_000_000 {
			number /= 1000
		}
		return time.Unix(number, 0).UTC().Format(time.RFC3339)
	}
	return value
}
