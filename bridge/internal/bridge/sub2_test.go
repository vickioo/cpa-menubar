package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSub2AccountSelectionAndSanitization(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		row     sub2AccountRow
		include bool
	}{
		{"valid account", sub2AccountRow{id: 1, name: "valid@example.com", status: "active", schedulable: true}, true},
		{"disabled account", sub2AccountRow{id: 2, name: "ordinary", status: "disabled"}, false},
		{"temporarily blocked", sub2AccountRow{id: 3, name: "blocked", status: "active", schedulable: true, tempUnschedulableUntil: sql.NullTime{Valid: true, Time: now.Add(time.Hour)}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account, include := sub2AccountFromRow(tt.row, now)
			if include != tt.include {
				t.Fatalf("include=%v, want %v", include, tt.include)
			}
			if include && account.Focus {
				t.Fatal("server-side focus must stay disabled in Sub2 mode")
			}
		})
	}
	account, _ := sub2AccountFromRow(tests[0].row, now)
	encoded, err := json.Marshal(account)
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"valid@example.com", "credentials", "\"access_token\":", "\"refresh_token\":", "password"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, payload)
		}
	}
	if account.DisplayName != "va***@example.com" {
		t.Fatalf("unexpected mask: %q", account.DisplayName)
	}
}

func TestSub2AccountMapsNativeQuotaWithoutRawJSON(t *testing.T) {
	used5h, used7d, limit, usage := 5.0, 19.0, 800.0, 72.0
	row := sub2AccountRow{
		id: 20, name: "main", accountType: "oauth", status: "active", schedulable: true,
		native: sub2NativeUsage{
			planType:     sql.NullString{String: "plus", Valid: true},
			fiveHourUsed: sql.NullFloat64{Float64: used5h, Valid: true},
			weeklyUsed:   sql.NullFloat64{Float64: used7d, Valid: true},
			weeklyLimit:  sql.NullFloat64{Float64: limit, Valid: true},
			weeklyUsage:  sql.NullFloat64{Float64: usage, Valid: true},
		},
	}
	account, include := sub2AccountFromRow(row, time.Now())
	if !include || account.Plan != "plus" || account.AuthorizationType != "oauth" {
		t.Fatalf("unexpected mapped account: %+v", account)
	}
	if account.FiveHourUsed == nil || *account.FiveHourUsed != used5h || account.WeeklyUsed == nil || *account.WeeklyUsed != used7d {
		t.Fatalf("native quota was not mapped: %+v", account)
	}
	encoded, err := json.Marshal(account)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"\"credentials\":", "\"extra\":", "\"access_token\":", "\"refresh_token\":"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("response leaked %q", forbidden)
		}
	}
}

func TestSub2PublicIDIsStableAndOpaque(t *testing.T) {
	if got := sub2PublicID(20); got != sub2PublicID(20) {
		t.Fatal("public ID is not stable")
	}
	if got := sub2PublicID(20); len(got) != 12 || got == "20" {
		t.Fatalf("public ID is not opaque: %q", got)
	}
}

func TestFetchSub2ProbeReturnsOnlyLiveQuotaSignals(t *testing.T) {
	checkedAt := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	requests := 0
	server := &Server{
		cfg: Config{UsageTimeout: time.Second},
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.Header.Get("Authorization") != "Bearer access-secret" || request.Header.Get("Chatgpt-Account-Id") != "acct-secret" {
				t.Fatal("probe request did not contain required upstream authorization")
			}
			switch request.URL.String() {
			case sub2ProbeUsageURL:
				return jsonResponse(http.StatusOK, `{
					"rate_limit":{"primary_window":{"used_percent":42,"limit_window_seconds":18000,"reset_after_seconds":3600},"secondary_window":{"used_percent":13,"limit_window_seconds":604800,"reset_at":1780000000}},
					"rate_limit_reset_credits":{"available_count":1}
				}`), nil
			case sub2ProbeResetCreditsURL:
				return jsonResponse(http.StatusOK, `{"availableCount":"2","credits":[{"resetType":"codex_rate_limits","status":"available","expiresAt":"2026-08-03T12:00:00Z"}]}`), nil
			default:
				t.Fatalf("unexpected probe URL: %s", request.URL)
				return nil, nil
			}
		})},
	}
	account := sub2ProbeAccount{
		accessToken:      sql.NullString{String: "access-secret", Valid: true},
		chatGPTAccountID: sql.NullString{String: "acct-secret", Valid: true},
	}
	snapshot := server.fetchSub2Probe(context.Background(), account, checkedAt)
	if snapshot.status != "ok" || snapshot.authorizationAlive == nil || !*snapshot.authorizationAlive {
		t.Fatalf("unexpected authorization snapshot: %+v", snapshot)
	}
	if snapshot.fiveHourUsed == nil || *snapshot.fiveHourUsed != 42 || snapshot.weeklyUsed == nil || *snapshot.weeklyUsed != 13 {
		t.Fatalf("quota windows were not normalized: %+v", snapshot)
	}
	if snapshot.resetCredits == nil || *snapshot.resetCredits != 2 || snapshot.resetCreditExpiry != "2026-08-03T12:00:00Z" {
		t.Fatalf("reset credits were not merged: %+v", snapshot)
	}
	if requests != 2 {
		t.Fatalf("probe requests = %d, want 2", requests)
	}

	server.probeCache = map[int64]sub2ProbeSnapshot{20: snapshot}
	mapped := Account{AuthorizationType: "oauth"}
	server.applySub2Probe(&mapped, 20)
	encoded, err := json.Marshal(mapped)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, secret := range []string{"access-secret", "acct-secret", "credentials", "proxy_password"} {
		if strings.Contains(body, secret) {
			t.Fatalf("public probe response leaked %q: %s", secret, body)
		}
	}
}

func TestFetchSub2ProbeDistinguishesUnauthorizedFromTransientFailure(t *testing.T) {
	server := &Server{
		cfg: Config{UsageTimeout: time.Second},
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized, `{}`), nil
		})},
	}
	account := sub2ProbeAccount{
		accessToken:      sql.NullString{String: "access", Valid: true},
		chatGPTAccountID: sql.NullString{String: "account", Valid: true},
	}
	snapshot := server.fetchSub2Probe(context.Background(), account, time.Now())
	if snapshot.status != "unauthorized" || snapshot.authorizationAlive == nil || *snapshot.authorizationAlive {
		t.Fatalf("unauthorized response was not mapped safely: %+v", snapshot)
	}
}

func TestParseSub2ResetCreditDetailsHandlesListAndExplicitCount(t *testing.T) {
	count, expiry, present, err := parseSub2ResetCreditDetails([]byte(`{
		"available_count":3,
		"credits":[
			{"reset_type":"other","status":"available","expires_at":"2026-08-01T00:00:00Z"},
			{"reset_type":"codex_rate_limits","status":"available","expires_at":"2026-08-04T00:00:00Z"},
			{"reset_type":"codex_rate_limits","status":"available","expires_at":"2026-08-03T00:00:00Z"}
		]
	}`))
	if err != nil || !present || count == nil || *count != 3 || expiry != "2026-08-03T00:00:00Z" {
		t.Fatalf("unexpected reset credit parse: count=%v expiry=%q present=%v err=%v", count, expiry, present, err)
	}
}

func TestNormalizeSub2ExpirySupportsUnixAndRFC3339(t *testing.T) {
	if got := normalizeSub2Expiry("1780000000"); got != "2026-05-28T20:26:40Z" {
		t.Fatalf("unix expiry = %q", got)
	}
	if got := normalizeSub2Expiry("2026-08-14T12:30:00+08:00"); got != "2026-08-14T04:30:00Z" {
		t.Fatalf("RFC3339 expiry = %q", got)
	}
}

func TestApplySub2ProbePreservesCachedQuotaOnProbeError(t *testing.T) {
	used := 23.0
	server := &Server{probeCache: map[int64]sub2ProbeSnapshot{
		20: {status: "error", checkedAt: time.Now()},
	}}
	account := Account{AuthorizationType: "oauth", WeeklyUsed: &used}
	server.applySub2Probe(&account, 20)
	if account.WeeklyUsed == nil || *account.WeeklyUsed != used {
		t.Fatal("transient probe failure erased the cached quota")
	}
	if account.AuthorizationAlive != nil || account.AuthorizationProbeStatus != "error" {
		t.Fatalf("transient error was misclassified: %+v", account)
	}
}
