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
	cfg := Config{Sub2FocusPriority: 80, Sub2FocusGroupIDs: []string{"7"}}
	tests := []struct {
		name           string
		row            sub2AccountRow
		include, focus bool
	}{
		{"valid account", sub2AccountRow{id: 1, name: "valid@example.com", status: "active", schedulable: true}, true, false},
		{"invalid ordinary account", sub2AccountRow{id: 2, name: "ordinary", status: "disabled"}, false, false},
		{"priority focus", sub2AccountRow{id: 3, name: "priority", status: "disabled", priority: 80}, true, true},
		{"note focus", sub2AccountRow{id: 4, name: "note", status: "disabled", notes: "[focus] keep"}, true, true},
		{"chinese note focus", sub2AccountRow{id: 5, name: "note", status: "disabled", notes: "[重点]"}, true, true},
		{"group focus", sub2AccountRow{id: 6, name: "group", status: "disabled", groupIDs: []string{"7"}}, true, true},
		{"temporarily blocked", sub2AccountRow{id: 7, name: "blocked", status: "active", schedulable: true, tempUnschedulableUntil: sql.NullTime{Valid: true, Time: now.Add(time.Hour)}}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account, include := sub2AccountFromRow(tt.row, cfg, now)
			if include != tt.include {
				t.Fatalf("include=%v, want %v", include, tt.include)
			}
			if include && account.Focus != tt.focus {
				t.Fatalf("focus=%v, want %v", account.Focus, tt.focus)
			}
		})
	}
	account, _ := sub2AccountFromRow(tests[0].row, cfg, now)
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
