package bridge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const sub2AccountsQuery = `
WITH group_data AS (
  SELECT ag.account_id,
         array_agg(g.name::text ORDER BY g.name) AS group_names
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
SELECT a.id, a.name, a.platform, a.type, a.status, a.schedulable,
       a.expires_at, a.last_used_at, a.temp_unschedulable_until,
       coalesce(array_to_json(gd.group_names), '[]'::json) AS group_names,
       coalesce(ru.requests, 0), coalesce(re.errors, 0),
       u.plan_type, u.five_hour_used_percent, u.five_hour_reset_at,
       u.weekly_used_percent, u.weekly_reset_at,
       u.weekly_limit, u.weekly_usage, u.usage_updated_at,
       u.rate_limit_reset_at, u.ops_health
FROM accounts a
LEFT JOIN group_data gd ON gd.account_id = a.id
LEFT JOIN recent_usage ru ON ru.account_id = a.id
LEFT JOIN recent_errors re ON re.account_id = a.id
LEFT JOIN cpa_desktop_account_usage u ON u.account_id = a.id
WHERE a.deleted_at IS NULL
ORDER BY a.id`

const sub2NativeUsageQuery = `
SELECT plan_type, five_hour_used_percent, five_hour_reset_at,
       weekly_used_percent, weekly_reset_at, weekly_limit, weekly_usage,
       usage_updated_at, rate_limit_reset_at, ops_health
FROM cpa_desktop_account_usage
WHERE account_id = $1`

type sub2AccountRow struct {
	id                                            int64
	name, platform, accountType, status           string
	schedulable                                   bool
	expiresAt, lastUsedAt, tempUnschedulableUntil sql.NullTime
	groupNames                                    []string
	recentRequests, recentErrors                  int64
	native                                        sub2NativeUsage
}

type sub2NativeUsage struct {
	planType                                    sql.NullString
	fiveHourUsed, weeklyUsed                    sql.NullFloat64
	fiveHourResetAt, weeklyResetAt              sql.NullString
	weeklyLimit, weeklyUsage                    sql.NullFloat64
	usageUpdatedAt, rateLimitResetAt, opsHealth sql.NullString
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
		var groupNamesJSON []byte
		if err := rows.Scan(&row.id, &row.name, &row.platform, &row.accountType, &row.status,
			&row.schedulable, &row.expiresAt, &row.lastUsedAt,
			&row.tempUnschedulableUntil, &groupNamesJSON,
			&row.recentRequests, &row.recentErrors,
			&row.native.planType, &row.native.fiveHourUsed, &row.native.fiveHourResetAt,
			&row.native.weeklyUsed, &row.native.weeklyResetAt,
			&row.native.weeklyLimit, &row.native.weeklyUsage, &row.native.usageUpdatedAt,
			&row.native.rateLimitResetAt, &row.native.opsHealth); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(groupNamesJSON, &row.groupNames); err != nil {
			return nil, fmt.Errorf("decode Sub2 group names: %w", err)
		}
		account, include := sub2AccountFromRow(row, time.Now())
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
	pools := make([]PoolSummary, 0, 2)

	usage0703, found, err := s.loadSub2NativeUsage(r.Context(), int64(s.cfg.Sub2Target0703ID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read Sub2 pool summary")
		return
	}
	pool0703 := PoolSummary{ID: "0703", Name: "0703车队 20X主账号"}
	if found {
		pool0703.Accounts = 1
		pool0703.WeeklyUsedPercent = nullFloatPointer(usage0703.weeklyUsed)
		pool0703.WeeklyResetAt = nullStringValue(usage0703.weeklyResetAt)
	}
	pools = append(pools, pool0703)

	poolFu := PoolSummary{ID: "fuccc", Name: "福CCC 双镜像综合"}
	var limit, used float64
	var hasLimit, hasUsed bool
	for _, rawID := range s.cfg.Sub2TargetFuIDs {
		id, parseErr := strconv.ParseInt(rawID, 10, 64)
		if parseErr != nil {
			continue
		}
		usage, exists, loadErr := s.loadSub2NativeUsage(r.Context(), id)
		if loadErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read Sub2 pool summary")
			return
		}
		if !exists {
			continue
		}
		poolFu.Accounts++
		if usage.weeklyLimit.Valid {
			limit += usage.weeklyLimit.Float64
			hasLimit = true
		}
		if usage.weeklyUsage.Valid {
			used += usage.weeklyUsage.Float64
			hasUsed = true
		}
	}
	if hasLimit {
		poolFu.WeeklyLimit = &limit
	}
	if hasUsed {
		poolFu.WeeklyUsage = &used
	}
	if hasLimit && limit > 0 {
		percent := used / limit * 100
		poolFu.WeeklyUsedPercent = &percent
	}
	pools = append(pools, poolFu)

	writeJSON(w, http.StatusOK, PoolsResponse{GeneratedAt: time.Now(), Pools: pools})
}

func (s *Server) loadSub2NativeUsage(ctx context.Context, accountID int64) (sub2NativeUsage, bool, error) {
	var usage sub2NativeUsage
	err := s.db.QueryRowContext(ctx, sub2NativeUsageQuery, accountID).Scan(
		&usage.planType, &usage.fiveHourUsed, &usage.fiveHourResetAt,
		&usage.weeklyUsed, &usage.weeklyResetAt, &usage.weeklyLimit, &usage.weeklyUsage,
		&usage.usageUpdatedAt, &usage.rateLimitResetAt, &usage.opsHealth,
	)
	if err == sql.ErrNoRows {
		return sub2NativeUsage{}, false, nil
	}
	if err != nil {
		return sub2NativeUsage{}, false, err
	}
	return usage, true, nil
}

func sub2AccountFromRow(row sub2AccountRow, now time.Time) (Account, bool) {
	valid := row.status == "active" && row.schedulable &&
		(!row.expiresAt.Valid || row.expiresAt.Time.After(now)) &&
		(!row.tempUnschedulableUntil.Valid || !row.tempUnschedulableUntil.Time.After(now))
	if !valid {
		return Account{}, false
	}

	plan := row.accountType
	if row.native.planType.Valid && strings.TrimSpace(row.native.planType.String) != "" {
		plan = row.native.planType.String
	}
	account := Account{
		ID: sub2PublicID(row.id), Provider: "codex", Source: "sub2",
		DisplayName: maskIdentifier(row.name), Plan: plan, AuthorizationType: row.accountType,
		Disabled: false, Status: row.status, Schedulable: true, Valid: true,
		Groups: row.groupNames, UsageStatus: "ok",
		RecentRequests: row.recentRequests, RecentErrors: row.recentErrors,
		FiveHourUsed:     nullFloatPointer(row.native.fiveHourUsed),
		FiveHourResetAt:  nullStringValue(row.native.fiveHourResetAt),
		WeeklyUsed:       nullFloatPointer(row.native.weeklyUsed),
		WeeklyResetAt:    nullStringValue(row.native.weeklyResetAt),
		WeeklyLimit:      nullFloatPointer(row.native.weeklyLimit),
		WeeklyUsage:      nullFloatPointer(row.native.weeklyUsage),
		UsageUpdatedAt:   nullStringValue(row.native.usageUpdatedAt),
		RateLimitResetAt: nullStringValue(row.native.rateLimitResetAt),
		OpsHealth:        nullStringValue(row.native.opsHealth),
	}
	if row.expiresAt.Valid {
		account.ExpiredAt = row.expiresAt.Time.UTC().Format(time.RFC3339)
	}
	if row.lastUsedAt.Valid {
		account.LastUsedAt = row.lastUsedAt.Time.UTC().Format(time.RFC3339)
	}
	if row.recentErrors > 0 {
		account.UsageStatus = "attention"
	}
	return account, true
}

func nullFloatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func sub2PublicID(id int64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("sub2:%d", id)))
	return hex.EncodeToString(digest[:])[:12]
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
