package bridge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const sub2AccountsQuery = `
WITH group_data AS (
  SELECT ag.account_id,
         array_agg(g.name::text ORDER BY g.name) AS group_names,
         array_agg(ag.group_id::text ORDER BY ag.group_id) AS group_ids
  FROM account_groups ag
  JOIN groups g ON g.id = ag.group_id
  GROUP BY ag.account_id
), recent_usage AS (
  SELECT account_id, count(*)::bigint AS requests
  FROM usage_logs
  WHERE created_at >= now() - interval '24 hours'
  GROUP BY account_id
), recent_errors AS (
  SELECT account_id, count(*)::bigint AS errors
  FROM ops_error_logs
  WHERE created_at >= now() - interval '24 hours' AND account_id IS NOT NULL
  GROUP BY account_id
)
SELECT a.id, a.name, a.platform, a.type, a.status, a.schedulable, a.priority,
       a.expires_at, a.last_used_at, a.temp_unschedulable_until,
       coalesce(gd.group_names, ARRAY[]::text[]) AS group_names,
       coalesce(gd.group_ids, ARRAY[]::text[]) AS group_ids,
       coalesce(ru.requests, 0), coalesce(re.errors, 0),
       coalesce(a.notes, '')
FROM accounts a
LEFT JOIN group_data gd ON gd.account_id = a.id
LEFT JOIN recent_usage ru ON ru.account_id = a.id
LEFT JOIN recent_errors re ON re.account_id = a.id
WHERE a.deleted_at IS NULL
ORDER BY a.priority DESC, a.id`

type sub2AccountRow struct {
	id int64
	name, platform, accountType, status string
	schedulable bool
	priority int
	expiresAt, lastUsedAt, tempUnschedulableUntil sql.NullTime
	groupNames, groupIDs []string
	recentRequests, recentErrors int64
	notes string
}

func (s *Server) loadSub2Accounts(ctx context.Context) ([]Account, error) {
	if s.db == nil {
		return nil, fmt.Errorf("Sub2 database is not configured")
	}
	rows, err := s.db.QueryContext(ctx, sub2AccountsQuery)
	if err != nil { return nil, err }
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		var row sub2AccountRow
		if err := rows.Scan(&row.id, &row.name, &row.platform, &row.accountType, &row.status,
			&row.schedulable, &row.priority, &row.expiresAt, &row.lastUsedAt,
			&row.tempUnschedulableUntil, &row.groupNames, &row.groupIDs,
			&row.recentRequests, &row.recentErrors, &row.notes); err != nil { return nil, err }
		account, include := sub2AccountFromRow(row, s.cfg, time.Now())
		if include { accounts = append(accounts, account) }
	}
	return accounts, rows.Err()
}

func sub2AccountFromRow(row sub2AccountRow, cfg Config, now time.Time) (Account, bool) {
	valid := row.status == "active" && row.schedulable &&
		(!row.expiresAt.Valid || row.expiresAt.Time.After(now)) &&
		(!row.tempUnschedulableUntil.Valid || !row.tempUnschedulableUntil.Time.After(now))
	focus := row.priority >= cfg.Sub2FocusPriority || containsFocusMarker(row.notes) || intersects(row.groupIDs, cfg.Sub2FocusGroupIDs)
	if !valid && !focus { return Account{}, false }

	digest := sha256.Sum256([]byte(fmt.Sprintf("sub2:%d", row.id)))
	account := Account{
		ID: hex.EncodeToString(digest[:])[:12], Provider: "codex", Source: "sub2",
		DisplayName: maskIdentifier(row.name), Plan: row.accountType,
		Disabled: !row.schedulable, Status: row.status, Schedulable: row.schedulable,
		Valid: valid, Focus: focus, Priority: row.priority, Groups: row.groupNames,
		UsageStatus: "ok", RecentRequests: row.recentRequests, RecentErrors: row.recentErrors,
	}
	if row.expiresAt.Valid { account.ExpiredAt = row.expiresAt.Time.UTC().Format(time.RFC3339) }
	if row.lastUsedAt.Valid { account.LastUsedAt = row.lastUsedAt.Time.UTC().Format(time.RFC3339) }
	if row.recentErrors > 0 || !valid { account.UsageStatus = "attention" }
	return account, true
}

func containsFocusMarker(notes string) bool {
	lower := strings.ToLower(notes)
	return strings.Contains(lower, "[focus]") || strings.Contains(lower, "[重点]")
}

func intersects(values, wanted []string) bool {
	set := make(map[string]struct{}, len(wanted))
	for _, value := range wanted { set[value] = struct{}{} }
	for _, value := range values { if _, ok := set[value]; ok { return true } }
	return false
}

func maskIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "@") { return maskEmail(value) }
	if len([]rune(value)) <= 2 { return "**" }
	runes := []rune(value)
	return string(runes[:2]) + "***"
}
