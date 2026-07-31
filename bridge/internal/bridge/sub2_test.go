package bridge

import (
	"database/sql"
	"encoding/json"
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
