package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testDesktopToken = "0123456789abcdef0123456789abcdef"

func TestProtectedEndpointsRequireBearerToken(t *testing.T) {
	server := newTestServer(t, t.TempDir())

	for _, header := range []string{"", testDesktopToken, "Basic " + testDesktopToken, "Bearer wrong"} {
		request := httptest.NewRequest(http.MethodGet, "/desktop/v1/summary", nil)
		request.Header.Set("Authorization", header)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("header %q returned %d, want 401", header, response.Code)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/desktop/v1/summary", nil)
	request.Header.Set("Authorization", "bearer "+testDesktopToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid bearer token returned %d", response.Code)
	}
}

func TestAccountsResponseDoesNotLeakCredentials(t *testing.T) {
	authDir := t.TempDir()
	secretValues := []string{"access-secret", "refresh-secret", "id-secret"}
	payload := map[string]any{
		"type":          "codex",
		"email":         "person@example.com",
		"account_id":    "account-secret",
		"access_token":  secretValues[0],
		"refresh_token": secretValues[1],
		"id_token":      secretValues[2],
		"disabled":      true,
	}
	writeFixture(t, filepath.Join(authDir, "codex.json"), payload)
	server := newTestServer(t, authDir)

	request := httptest.NewRequest(http.MethodGet, "/desktop/v1/accounts", nil)
	request.Header.Set("Authorization", "Bearer "+testDesktopToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("accounts returned %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, secret := range append(secretValues, "account-secret", "person@example.com") {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "pe***@example.com") {
		t.Fatalf("response did not contain masked email: %s", body)
	}
}

func TestSanitizeUsageUsesAllowlistAndRemovesSensitiveNestedFields(t *testing.T) {
	input := []byte(`{
		"plan_type":"plus",
		"rate_limit":{"primary_window":{"used_percent":42,"access_token":"secret"}},
		"email":"person@example.com",
		"access_token":"top-secret",
		"other":{"value":1}
	}`)
	sanitized, err := sanitizeUsage(input)
	if err != nil {
		t.Fatal(err)
	}
	body := string(sanitized)
	for _, forbidden := range []string{"secret", "person@example.com", "other", "access_token"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("sanitized usage contains %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"used_percent":42`) {
		t.Fatalf("sanitized usage lost allowed data: %s", body)
	}
}

func TestReadEnvFileValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.env")
	if err := os.WriteFile(path, []byte("# ignored\nOTHER=value\nCPA_SMART_ROUTER_KEY='router-secret'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := readEnvFileValue(path, "CPA_SMART_ROUTER_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if value != "router-secret" {
		t.Fatalf("router key = %q", value)
	}
}

func TestFetchXAIUsageAggregatesSmartRouterMetrics(t *testing.T) {
	server := newTestServer(t, t.TempDir())
	server.cfg.RouterURL = "http://router.test"
	server.cfg.RouterAPIKey = "router-secret"
	server.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/debug/summary" || request.URL.Query().Get("hours") != "24" {
			t.Fatalf("unexpected router request: %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer router-secret" {
			t.Fatal("router request missing authorization")
		}
		return jsonResponse(http.StatusOK, `{
			"providers":[
				{"id":"cpa-grok-fast","route_model":"grok-3-mini-fast","upstream_model":"grok-3-mini-fast","tokens_today":1421,"daily_token_limit":1000000},
				{"id":"codex","route_model":"gpt-5.5","tokens_today":99,"daily_token_limit":1000}
			],
			"by_provider":[
				{"name":"cpa-grok-fast","requests":23,"successes":22,"errors":1,"input_tokens":4522,"output_tokens":3529,"total_tokens":8051,"last_request_at":"2026-07-27T13:20:53+08:00"},
				{"name":"codex","requests":100,"successes":100,"total_tokens":9000}
			],
			"recent_errors":[{"error":"must not be forwarded"}]
		}`), nil
	})}

	usage, status, err := server.fetchXAIUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status != "ok" {
		t.Fatalf("status = %q", status)
	}
	if usage.Requests != 23 || usage.Successes != 22 || usage.Errors != 1 || usage.TotalTokens != 8051 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if usage.TodayTokens != 1421 || usage.DailyTokenLimit != 1000000 {
		t.Fatalf("unexpected daily usage: %+v", usage)
	}
}

func TestFetchXAIUsageCombinesOfficialBillingAndSmartRouterMetrics(t *testing.T) {
	server := newTestServer(t, t.TempDir())
	server.cfg.RouterURL = "http://router.test"
	server.cfg.RouterAPIKey = "router-secret"
	server.cfg.XAIManagementURL = "https://management.test"
	server.cfg.XAIManagementAPIKey = "management-secret"
	server.cfg.XAIManagementTeamID = "team-123"
	server.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Host == "router.test":
			return jsonResponse(http.StatusOK, `{
				"providers":[{"id":"xai-main","route_model":"grok-4","tokens_today":40,"daily_token_limit":100}],
				"by_provider":[{"name":"xai-main","requests":4,"successes":4,"input_tokens":30,"output_tokens":10,"total_tokens":40}]
			}`), nil
		case request.URL.Host == "management.test" && request.URL.Path == "/v1/billing/teams/team-123/usage":
			if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer management-secret" {
				t.Fatalf("unexpected official usage request: %s %s", request.Method, request.Header.Get("Authorization"))
			}
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			analyticsRequest, ok := body["analyticsRequest"].(map[string]any)
			if !ok || analyticsRequest["timeUnit"] != "TIME_UNIT_DAY" {
				t.Fatalf("unexpected analytics request: %#v", body)
			}
			return jsonResponse(http.StatusOK, `{
				"timeSeries":[{"dataPoints":[{"values":[0.25]},{"values":[0.5]}]}],
				"limitReached":false
			}`), nil
		case request.URL.Host == "management.test" && request.URL.Path == "/v1/billing/teams/team-123/prepaid/balance":
			if request.Method != http.MethodGet {
				t.Fatalf("unexpected balance method: %s", request.Method)
			}
			return jsonResponse(http.StatusOK, `{"availableBalance":12.34,"hasPrepaidCredit":true}`), nil
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
			return nil, nil
		}
	})}

	usage, status, err := server.fetchXAIUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status != "ok" || usage.OfficialStatus != "ok" || usage.CPAStatus != "ok" {
		t.Fatalf("unexpected statuses: overall=%q usage=%+v", status, usage)
	}
	if usage.OfficialSpendUSD == nil || *usage.OfficialSpendUSD != 0.75 {
		t.Fatalf("official spend = %#v", usage.OfficialSpendUSD)
	}
	if usage.PrepaidBalanceUSD == nil || *usage.PrepaidBalanceUSD != 12.34 || usage.HasPrepaidCredit == nil || !*usage.HasPrepaidCredit {
		t.Fatalf("unexpected prepaid balance: %+v", usage)
	}
	if usage.TotalTokens != 40 || usage.TodayTokens != 40 || usage.DailyTokenLimit != 100 {
		t.Fatalf("unexpected CPA routing usage: %+v", usage)
	}
}

func TestSummaryIncludesSanitizedXAIUsage(t *testing.T) {
	authDir := t.TempDir()
	writeFixture(t, filepath.Join(authDir, "xai.json"), map[string]any{
		"type":          "xai",
		"email":         "person@example.com",
		"access_token":  "xai-access-secret",
		"refresh_token": "xai-refresh-secret",
	})
	server := newTestServer(t, authDir)
	server.cfg.RouterURL = "http://router.test"
	server.cfg.RouterAPIKey = "router-secret"
	server.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"providers":[{"id":"xai-main","route_model":"grok-3-mini-fast","tokens_today":10,"daily_token_limit":100}],
			"by_provider":[{"name":"xai-main","requests":2,"successes":2,"input_tokens":7,"output_tokens":3,"total_tokens":10}]
		}`), nil
	})}

	request := httptest.NewRequest(http.MethodGet, "/desktop/v1/summary", nil)
	request.Header.Set("Authorization", "Bearer "+testDesktopToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("summary returned %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, forbidden := range []string{"router-secret", "xai-access-secret", "xai-refresh-secret", "person@example.com"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("summary leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"xai_usage_status":"ok"`) || !strings.Contains(body, `"total_tokens":10`) {
		t.Fatalf("summary missing xAI usage: %s", body)
	}
}

func TestOAuthSessionRejectsExpiredState(t *testing.T) {
	server := newTestServer(t, t.TempDir())
	server.sessions["expired"] = &oauthSession{
		State:     "expired",
		Verifier:  "verifier",
		ExpiresAt: time.Now().Add(-time.Minute),
		Status:    "pending",
	}

	request := httptest.NewRequest(http.MethodPost, "/desktop/v1/oauth/codex/callback", strings.NewReader(`{"state":"expired","code":"code"}`))
	request.Header.Set("Authorization", "Bearer "+testDesktopToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expired OAuth state returned %d", response.Code)
	}
}

func TestExchangeAndSavePreservesOperationalFields(t *testing.T) {
	authDir := t.TempDir()
	existingPath := filepath.Join(authDir, "codex-existing.json")
	writeFixture(t, existingPath, map[string]any{
		"account_id":      "acct-1",
		"priority":        9,
		"excluded-models": []string{"example-model"},
		"prefix":          "preferred",
		"proxy_url":       "http://127.0.0.1:9999",
	})

	server := newTestServer(t, authDir)
	server.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != codexTokenURL {
			t.Fatalf("unexpected token URL: %s", request.URL)
		}
		response := tokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			IDToken:      testJWT(t, "person@example.com", "acct-1", "plus"),
			ExpiresIn:    3600,
		}
		data, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(data))),
			Header:     make(http.Header),
		}, nil
	})}

	masked, err := server.exchangeAndSave(context.Background(), "code", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if masked != "pe***@example.com" {
		t.Fatalf("masked email = %q", masked)
	}
	data, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["access_token"] != "new-access" || saved["refresh_token"] != "new-refresh" {
		t.Fatalf("new credentials not saved")
	}
	if saved["priority"] != float64(9) || saved["prefix"] != "preferred" || saved["proxy_url"] != "http://127.0.0.1:9999" {
		t.Fatalf("operational fields not preserved: %#v", saved)
	}
	info, err := os.Stat(existingPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("auth file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestReadLimitedRejectsOversizedResponses(t *testing.T) {
	if _, err := readLimited(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("expected oversized response error")
	}
	data, err := readLimited(strings.NewReader("1234"), 4)
	if err != nil || string(data) != "1234" {
		t.Fatalf("exact-limit response = %q, %v", data, err)
	}
}

func TestResetCreditRequiresExplicitConfirmation(t *testing.T) {
	authDir := t.TempDir()
	writeFixture(t, filepath.Join(authDir, "codex.json"), map[string]any{
		"type":         "codex",
		"account_id":   "acct-1",
		"access_token": "access",
	})
	server := newTestServer(t, authDir)
	server.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("reset endpoint contacted without explicit confirmation")
		return nil, nil
	})}

	request := httptest.NewRequest(http.MethodPost, "/desktop/v1/accounts/"+accountPublicID("codex.json")+"/reset-credit", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+testDesktopToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing reset confirmation returned %d", response.Code)
	}
}

func TestResetCreditRejectsWhenNoApplicableCreditExists(t *testing.T) {
	authDir := t.TempDir()
	writeFixture(t, filepath.Join(authDir, "codex.json"), map[string]any{
		"type":         "codex",
		"account_id":   "acct-1",
		"access_token": "access",
	})
	server := newTestServer(t, authDir)
	server.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet {
			t.Fatalf("unexpected write request when no applicable credit exists: %s", request.Method)
		}
		return jsonResponse(http.StatusOK, `{"rate_limit_reset_credits":{"available_count":2,"applicable_available_count":0}}`), nil
	})}

	response := performResetRequest(server, accountPublicID("codex.json"))
	if response.Code != http.StatusConflict {
		t.Fatalf("unavailable reset credit returned %d: %s", response.Code, response.Body.String())
	}
}

func TestResetCreditConsumesOneApplicableCredit(t *testing.T) {
	authDir := t.TempDir()
	writeFixture(t, filepath.Join(authDir, "codex.json"), map[string]any{
		"type":         "codex",
		"account_id":   "acct-1",
		"access_token": "access",
	})
	server := newTestServer(t, authDir)
	requests := 0
	server.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch requests {
		case 1:
			if request.Method != http.MethodGet {
				t.Fatalf("first request method = %s", request.Method)
			}
			return jsonResponse(http.StatusOK, `{"rate_limit_reset_credits":{"available_count":2,"applicable_available_count":1}}`), nil
		case 2:
			if request.Method != http.MethodPost || request.URL.String() != codexResetCreditsURL {
				t.Fatalf("unexpected reset request: %s %s", request.Method, request.URL)
			}
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body["redeem_request_id"]) != 36 {
				t.Fatalf("invalid redeem request id: %q", body["redeem_request_id"])
			}
			return jsonResponse(http.StatusOK, `{}`), nil
		default:
			t.Fatalf("unexpected request count: %d", requests)
			return nil, nil
		}
	})}

	response := performResetRequest(server, accountPublicID("codex.json"))
	if response.Code != http.StatusOK {
		t.Fatalf("reset returned %d: %s", response.Code, response.Body.String())
	}
	if requests != 2 {
		t.Fatalf("request count = %d, want 2", requests)
	}
}

func newTestServer(t *testing.T, authDir string) *Server {
	t.Helper()
	server, err := NewServer(Config{
		AuthDir:      authDir,
		DesktopToken: testDesktopToken,
		CLIProxyURL:  "http://127.0.0.1:1",
		RouterURL:    "http://127.0.0.1:1",
		UsageTimeout: time.Second,
		OAuthTimeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func writeFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func performResetRequest(server *Server, accountID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(
		http.MethodPost,
		"/desktop/v1/accounts/"+accountID+"/reset-credit",
		strings.NewReader(`{"confirm":"CONSUME_RESET_CREDIT"}`),
	)
	request.Header.Set("Authorization", "Bearer "+testDesktopToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func testJWT(t *testing.T, email, accountID, plan string) string {
	t.Helper()
	payload := map[string]any{
		"email": email,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
			"chatgpt_plan_type":  plan,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	return encode([]byte(`{"alg":"none"}`)) + "." + encode(data) + ".signature"
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
