package bridge

import (
	"encoding/json"
	"time"
)

type Account struct {
	ID                       string          `json:"id"`
	Provider                 string          `json:"provider"`
	EmailMasked              string          `json:"email_masked"`
	Plan                     string          `json:"plan,omitempty"`
	Disabled                 bool            `json:"disabled"`
	ExpiredAt                string          `json:"expired_at,omitempty"`
	LastRefreshAt            string          `json:"last_refresh_at,omitempty"`
	HasRefreshToken          bool            `json:"has_refresh_token"`
	IsK12                    bool            `json:"is_k12"`
	Usage                    json.RawMessage `json:"usage,omitempty"`
	UsageStatus              string          `json:"usage_status"`
	UsageError               string          `json:"usage_error,omitempty"`
	SubscriptionUntil        string          `json:"subscription_until,omitempty"`
	Source                   string          `json:"source,omitempty"`
	DisplayName              string          `json:"display_name,omitempty"`
	AuthorizationType        string          `json:"authorization_type,omitempty"`
	Status                   string          `json:"status,omitempty"`
	Schedulable              bool            `json:"schedulable"`
	Valid                    bool            `json:"valid"`
	Focus                    bool            `json:"focus"`
	Groups                   []string        `json:"groups,omitempty"`
	LastUsedAt               string          `json:"last_used_at,omitempty"`
	RecentRequests           int64           `json:"recent_requests,omitempty"`
	RecentErrors             int64           `json:"recent_errors,omitempty"`
	FiveHourUsed             *float64        `json:"five_hour_used_percent,omitempty"`
	FiveHourResetAt          string          `json:"five_hour_reset_at,omitempty"`
	WeeklyUsed               *float64        `json:"weekly_used_percent,omitempty"`
	WeeklyResetAt            string          `json:"weekly_reset_at,omitempty"`
	WeeklyLimit              *float64        `json:"weekly_limit,omitempty"`
	WeeklyUsage              *float64        `json:"weekly_usage,omitempty"`
	UsageUpdatedAt           string          `json:"usage_updated_at,omitempty"`
	RateLimitResetAt         string          `json:"rate_limit_reset_at,omitempty"`
	OpsHealth                string          `json:"ops_health,omitempty"`
	AutomaticExpiryAt        string          `json:"automatic_expiry_at,omitempty"`
	ResetCredits             *int            `json:"reset_credits_available,omitempty"`
	ResetCreditExpiry        string          `json:"reset_credit_expiry_at,omitempty"`
	AuthorizationAlive       *bool           `json:"authorization_alive,omitempty"`
	AuthorizationCheckedAt   string          `json:"authorization_checked_at,omitempty"`
	AuthorizationProbeStatus string          `json:"authorization_probe_status,omitempty"`
}

type Summary struct {
	Source         string          `json:"source,omitempty"`
	GeneratedAt    time.Time       `json:"generated_at"`
	AccountsTotal  int             `json:"accounts_total"`
	CodexAccounts  int             `json:"codex_accounts"`
	XAIAccounts    int             `json:"xai_accounts"`
	K12Accounts    int             `json:"k12_accounts"`
	Refreshable    int             `json:"refreshable_accounts"`
	Expired        int             `json:"expired_accounts"`
	ValidAccounts  int             `json:"valid_accounts"`
	FocusAccounts  int             `json:"focus_accounts"`
	Services       map[string]bool `json:"services"`
	XAIUsage       *XAIUsage       `json:"xai_usage,omitempty"`
	XAIUsageStatus string          `json:"xai_usage_status"`
	XAIUsageError  string          `json:"xai_usage_error,omitempty"`
}

type XAIUsage struct {
	OfficialStatus       string   `json:"official_status"`
	OfficialError        string   `json:"official_error,omitempty"`
	OfficialPeriodStart  string   `json:"official_period_start,omitempty"`
	OfficialPeriodEnd    string   `json:"official_period_end,omitempty"`
	OfficialSpendUSD     *float64 `json:"official_spend_usd,omitempty"`
	PrepaidBalanceUSD    *float64 `json:"prepaid_balance_usd,omitempty"`
	HasPrepaidCredit     *bool    `json:"has_prepaid_credit,omitempty"`
	OfficialLimitReached bool     `json:"official_limit_reached"`
	CPAStatus            string   `json:"cpa_status"`
	CPAError             string   `json:"cpa_error,omitempty"`
	WindowHours          int      `json:"window_hours"`
	Requests             int64    `json:"requests"`
	Successes            int64    `json:"successes"`
	Errors               int64    `json:"errors"`
	InputTokens          int64    `json:"input_tokens"`
	OutputTokens         int64    `json:"output_tokens"`
	TotalTokens          int64    `json:"total_tokens"`
	TodayTokens          int64    `json:"today_tokens"`
	DailyTokenLimit      int64    `json:"daily_token_limit"`
	LastRequestAt        string   `json:"last_request_at,omitempty"`
}

type AccountsResponse struct {
	GeneratedAt time.Time `json:"generated_at"`
	Accounts    []Account `json:"accounts"`
}

type AccountDetail struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type PoolSummary struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Accounts          int64    `json:"accounts"`
	WeeklyLimit       *float64 `json:"weekly_limit,omitempty"`
	WeeklyUsage       *float64 `json:"weekly_usage,omitempty"`
	WeeklyUsedPercent *float64 `json:"weekly_used_percent,omitempty"`
	WeeklyResetAt     string   `json:"weekly_reset_at,omitempty"`
}

type PoolsResponse struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Pools       []PoolSummary `json:"pools"`
}

type RefreshResponse struct {
	StartedAt         time.Time `json:"started_at"`
	CompletedAt       time.Time `json:"completed_at"`
	Attempted         int       `json:"attempted"`
	Succeeded         int       `json:"succeeded"`
	Unauthorized      int       `json:"unauthorized"`
	Failed            int       `json:"failed"`
	SkippedByCooldown int       `json:"skipped_by_cooldown"`
	ResetCreditsRead  int       `json:"reset_credits_read"`
}

type OAuthStartResponse struct {
	State       string    `json:"state"`
	URL         string    `json:"url"`
	CallbackURL string    `json:"callback_url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type OAuthCallbackRequest struct {
	State       string `json:"state"`
	Code        string `json:"code"`
	RedirectURL string `json:"redirect_url"`
}

type OAuthStatusResponse struct {
	State         string `json:"state"`
	Status        string `json:"status"`
	Message       string `json:"message,omitempty"`
	AccountMasked string `json:"account_masked,omitempty"`
}

type ResetCreditRequest struct {
	Confirm string `json:"confirm"`
}

type ResetCreditResponse struct {
	AccountID string `json:"account_id"`
	Status    string `json:"status"`
}
