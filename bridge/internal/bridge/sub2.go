package bridge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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
	       coalesce(array_to_json(gd.group_names), '[]'::json) AS group_names,
	       coalesce(array_to_json(gd.group_ids), '[]'::json) AS group_ids,
       coalesce(ru.requests, 0), coalesce(re.errors, 0),
       coalesce(a.notes, '')
FROM accounts a
LEFT JOIN group_data gd ON gd.account_id = a.id
LEFT JOIN recent_usage ru ON ru.account_id = a.id
LEFT JOIN recent_errors re ON re.account_id = a.id
WHERE a.deleted_at IS NULL
ORDER BY a.priority DESC, a.id`

const sub2PoolsQuery = `
WITH account_counts AS (
  SELECT group_id, count(DISTINCT account_id)::bigint AS accounts
  FROM account_groups
  GROUP BY group_id
), weekly_usage AS (
  SELECT group_id, coalesce(sum(actual_cost), 0)::double precision AS usage
  FROM usage_logs
  WHERE created_at >= date_trunc('week', now()) AND group_id IS NOT NULL
  GROUP BY group_id
)
SELECT g.id::text, g.name, coalesce(ac.accounts, 0),
       coalesce(g.weekly_limit_usd, 0)::double precision,
       coalesce(wu.usage, 0)
FROM groups g
LEFT JOIN account_counts ac ON ac.group_id = g.id
LEFT JOIN weekly_usage wu ON wu.group_id = g.id
WHERE g.id IN (12, 13)
ORDER BY g.id`

type sub2AccountRow struct {
	id                                            int64
	name, platform, accountType, status           string
	schedulable                                   bool
	priority                                      int
	expiresAt, lastUsedAt, tempUnschedulableUntil sql.NullTime
	groupNames, groupIDs                          []string
	recentRequests, recentErrors                  int64
	notes                                         string
}

func (s *Server) loadSub2Accounts(ctx context.Context) ([]Account, error) {
	if s.db == nil {
		return nil, fmt.Errorf("Sub2 database is not configured")
	}
	rows, err := s.db.QueryContext(ctx, sub2AccountsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		var row sub2AccountRow
		var groupNamesJSON, groupIDsJSON []byte
		if err := rows.Scan(&row.id, &row.name, &row.platform, &row.accountType, &row.status,
			&row.schedulable, &row.priority, &row.expiresAt, &row.lastUsedAt,
			&row.tempUnschedulableUntil, &groupNamesJSON, &groupIDsJSON,
			&row.recentRequests, &row.recentErrors, &row.notes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(groupNamesJSON, &row.groupNames); err != nil {
			return nil, fmt.Errorf("decode Sub2 group names: %w", err)
		}
		if err := json.Unmarshal(groupIDsJSON, &row.groupIDs); err != nil {
			return nil, fmt.Errorf("decode Sub2 group IDs: %w", err)
		}
		account, include := sub2AccountFromRow(row, s.cfg, time.Now())
		if include {
			accounts = append(accounts, account)
		}
	}
	return accounts, rows.Err()
}

func (s *Server) handleSub2AccountDetail(w http.ResponseWriter, r *http.Request) {
	publicID := strings.TrimPrefix(r.URL.Path, "/desktop/v1/accounts/")
	if len(publicID) != 12 || strings.Contains(publicID, "/") {
		writeError(w, http.StatusBadRequest, "invalid account ID")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name FROM accounts WHERE deleted_at IS NULL`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read account detail")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read account detail")
			return
		}
		if sub2PublicID(id) == publicID {
			writeJSON(w, http.StatusOK, AccountDetail{ID: publicID, DisplayName: name})
			return
		}
	}
	writeError(w, http.StatusNotFound, "account not found")
}

func (s *Server) handleSub2Pools(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), sub2PoolsQuery)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read Sub2 pool summary")
		return
	}
	defer rows.Close()
	pools := make([]PoolSummary, 0, 2)
	for rows.Next() {
		var pool PoolSummary
		if err := rows.Scan(&pool.ID, &pool.Name, &pool.Accounts, &pool.WeeklyLimitUSD, &pool.WeeklyUsageUSD); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read Sub2 pool summary")
			return
		}
		pools = append(pools, pool)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read Sub2 pool summary")
		return
	}
	writeJSON(w, http.StatusOK, PoolsResponse{GeneratedAt: time.Now(), Pools: pools})
}

func sub2AccountFromRow(row sub2AccountRow, cfg Config, now time.Time) (Account, bool) {
	valid := row.status == "active" && row.schedulable &&
		(!row.expiresAt.Valid || row.expiresAt.Time.After(now)) &&
		(!row.tempUnschedulableUntil.Valid || !row.tempUnschedulableUntil.Time.After(now))
	focus := row.priority >= cfg.Sub2FocusPriority || containsFocusMarker(row.notes) || intersects(row.groupIDs, cfg.Sub2FocusGroupIDs)
	if !valid && !focus {
		return Account{}, false
	}

	account := Account{
		ID: sub2PublicID(row.id), Provider: "codex", Source: "sub2",
		DisplayName: maskIdentifier(row.name), Plan: row.accountType,
		Disabled: !row.schedulable, Status: row.status, Schedulable: row.schedulable,
		Valid: valid, Focus: focus, Priority: row.priority, Groups: row.groupNames,
		UsageStatus: "ok", RecentRequests: row.recentRequests, RecentErrors: row.recentErrors,
	}
	if row.expiresAt.Valid {
		account.ExpiredAt = row.expiresAt.Time.UTC().Format(time.RFC3339)
	}
	if row.lastUsedAt.Valid {
		account.LastUsedAt = row.lastUsedAt.Time.UTC().Format(time.RFC3339)
	}
	if row.recentErrors > 0 || !valid {
		account.UsageStatus = "attention"
	}
	return account, true
}

func sub2PublicID(id int64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("sub2:%d", id)))
	return hex.EncodeToString(digest[:])[:12]
}

func containsFocusMarker(notes string) bool {
	lower := strings.ToLower(notes)
	return strings.Contains(lower, "[focus]") || strings.Contains(lower, "[重点]")
}

func intersects(values, wanted []string) bool {
	set := make(map[string]struct{}, len(wanted))
	for _, value := range wanted {
		set[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

func maskIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "@") {
		return maskEmail(value)
	}
	if len([]rune(value)) <= 2 {
		return "**"
	}
	runes := []rune(value)
	return string(runes[:2]) + "***"
}
