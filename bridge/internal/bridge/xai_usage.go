package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	urlpkg "net/url"
	"strings"
	"time"
)

const xaiUsageWindowHours = 24

type routerSummary struct {
	Providers  []routerProviderDebug `json:"providers"`
	ByProvider []routerUsageBucket   `json:"by_provider"`
	ByModel    []routerUsageBucket   `json:"by_model"`
}

type routerProviderDebug struct {
	ID              string `json:"id"`
	RouteModel      string `json:"route_model"`
	UpstreamModel   string `json:"upstream_model"`
	TokensToday     int64  `json:"tokens_today"`
	DailyTokenLimit int64  `json:"daily_token_limit"`
}

type routerUsageBucket struct {
	Name          string  `json:"name"`
	Requests      int64   `json:"requests"`
	Successes     int64   `json:"successes"`
	Errors        int64   `json:"errors"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	TotalTokens   int64   `json:"total_tokens"`
	LastRequestAt string  `json:"last_request_at"`
	SuccessRate   float64 `json:"success_rate"`
}

type xaiOfficialUsage struct {
	PeriodStart    string
	PeriodEnd      string
	SpendUSD       float64
	PrepaidBalance *float64
	HasPrepaid     *bool
	LimitReached   bool
}

type xaiAnalyticsResponse struct {
	TimeSeries []struct {
		DataPoints []struct {
			Values []float64 `json:"values"`
		} `json:"dataPoints"`
	} `json:"timeSeries"`
	LimitReached bool `json:"limitReached"`
}

type xaiPrepaidBalanceResponse struct {
	AvailableBalance float64 `json:"availableBalance"`
	HasPrepaidCredit bool    `json:"hasPrepaidCredit"`
}

func (s *Server) hasXAIOfficialConfig() bool {
	return strings.TrimSpace(s.cfg.XAIManagementAPIKey) != "" && strings.TrimSpace(s.cfg.XAIManagementTeamID) != ""
}

func (s *Server) fetchXAIUsage(ctx context.Context) (*XAIUsage, string, error) {
	usage := &XAIUsage{WindowHours: xaiUsageWindowHours}

	routerUsage, routerStatus, routerErr := s.fetchXAIRouterUsage(ctx)
	usage.CPAStatus = routerStatus
	if routerUsage != nil {
		copyXAIRouterUsage(usage, *routerUsage)
	}
	if routerErr != nil {
		usage.CPAError = publicRouterUsageError(routerErr)
	}

	officialUsage, officialStatus, officialErr := s.fetchXAIOfficialUsage(ctx, time.Now())
	usage.OfficialStatus = officialStatus
	if officialUsage != nil {
		usage.OfficialPeriodStart = officialUsage.PeriodStart
		usage.OfficialPeriodEnd = officialUsage.PeriodEnd
		usage.OfficialSpendUSD = &officialUsage.SpendUSD
		usage.PrepaidBalanceUSD = officialUsage.PrepaidBalance
		usage.HasPrepaidCredit = officialUsage.HasPrepaid
		usage.OfficialLimitReached = officialUsage.LimitReached
	}
	if officialErr != nil {
		usage.OfficialError = publicXAIOfficialUsageError(officialErr)
	}

	officialAvailable := officialStatus == "ok" || officialStatus == "partial"
	routerAvailable := routerStatus == "ok"
	switch {
	case officialAvailable && routerAvailable:
		return usage, "ok", nil
	case routerAvailable && officialStatus == "not_configured":
		return usage, "ok", nil
	case officialAvailable || routerAvailable:
		return usage, "partial", nil
	default:
		return usage, "unavailable", errors.New("xAI usage sources unavailable")
	}
}

func (s *Server) fetchXAIRouterUsage(ctx context.Context) (*XAIUsage, string, error) {
	if strings.TrimSpace(s.cfg.RouterAPIKey) == "" {
		return nil, "unavailable", errors.New("router API key unavailable")
	}
	endpoint := strings.TrimRight(s.cfg.RouterURL, "/") + "/debug/summary?hours=24"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "error", err
	}
	request.Header.Set("Authorization", "Bearer "+s.cfg.RouterAPIKey)
	request.Header.Set("Accept", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return nil, "error", err
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body, 512*1024)
	if err != nil {
		return nil, "error", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Sprintf("http_%d", response.StatusCode), fmt.Errorf("router usage request returned %d", response.StatusCode)
	}

	var summary routerSummary
	if err = json.Unmarshal(data, &summary); err != nil {
		return nil, "error", err
	}
	usage, ok := aggregateXAIUsage(summary)
	if !ok {
		return nil, "unavailable", errors.New("xAI provider usage unavailable")
	}
	return &usage, "ok", nil
}

func (s *Server) fetchXAIOfficialUsage(ctx context.Context, now time.Time) (*xaiOfficialUsage, string, error) {
	if !s.hasXAIOfficialConfig() {
		return nil, "not_configured", nil
	}

	location := time.FixedZone("CST", 8*60*60)
	current := now.In(location)
	periodStart := time.Date(current.Year(), current.Month(), 1, 0, 0, 0, 0, location)
	requestBody := map[string]any{
		"analyticsRequest": map[string]any{
			"timeRange": map[string]string{
				"startTime": periodStart.Format("2006-01-02 15:04:05"),
				"endTime":   current.Format("2006-01-02 15:04:05"),
				"timezone":  "Asia/Shanghai",
			},
			"timeUnit": "TIME_UNIT_DAY",
			"values": []map[string]string{{
				"name":        "usd",
				"aggregation": "AGGREGATION_SUM",
			}},
			"groupBy": []string{},
			"filters": []any{},
		},
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, "error", err
	}

	teamID := urlpkg.PathEscape(s.cfg.XAIManagementTeamID)
	usageEndpoint := strings.TrimRight(s.cfg.XAIManagementURL, "/") + "/v1/billing/teams/" + teamID + "/usage"
	usageData, err := s.doXAIManagementRequest(ctx, http.MethodPost, usageEndpoint, payload)
	if err != nil {
		return nil, "error", err
	}
	var analytics xaiAnalyticsResponse
	if err = json.Unmarshal(usageData, &analytics); err != nil {
		return nil, "error", err
	}
	official := &xaiOfficialUsage{
		PeriodStart:  periodStart.Format(time.RFC3339),
		PeriodEnd:    current.Format(time.RFC3339),
		SpendUSD:     sumXAIAnalyticsValues(analytics),
		LimitReached: analytics.LimitReached,
	}

	balanceEndpoint := strings.TrimRight(s.cfg.XAIManagementURL, "/") + "/v1/billing/teams/" + teamID + "/prepaid/balance"
	balanceData, balanceErr := s.doXAIManagementRequest(ctx, http.MethodGet, balanceEndpoint, nil)
	if balanceErr != nil {
		return official, "partial", balanceErr
	}
	var balance xaiPrepaidBalanceResponse
	if err = json.Unmarshal(balanceData, &balance); err != nil {
		return official, "partial", err
	}
	official.PrepaidBalance = &balance.AvailableBalance
	official.HasPrepaid = &balance.HasPrepaidCredit
	return official, "ok", nil
}

func (s *Server) doXAIManagementRequest(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+s.cfg.XAIManagementAPIKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body, 512*1024)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("xAI management request returned %d", response.StatusCode)
	}
	return data, nil
}

func sumXAIAnalyticsValues(response xaiAnalyticsResponse) float64 {
	var total float64
	for _, series := range response.TimeSeries {
		for _, point := range series.DataPoints {
			for _, value := range point.Values {
				total += value
			}
		}
	}
	return total
}

func copyXAIRouterUsage(target *XAIUsage, source XAIUsage) {
	target.WindowHours = source.WindowHours
	target.Requests = source.Requests
	target.Successes = source.Successes
	target.Errors = source.Errors
	target.InputTokens = source.InputTokens
	target.OutputTokens = source.OutputTokens
	target.TotalTokens = source.TotalTokens
	target.TodayTokens = source.TodayTokens
	target.DailyTokenLimit = source.DailyTokenLimit
	target.LastRequestAt = source.LastRequestAt
}

func aggregateXAIUsage(summary routerSummary) (XAIUsage, bool) {
	usage := XAIUsage{WindowHours: xaiUsageWindowHours}
	providerIDs := make(map[string]struct{})
	for _, provider := range summary.Providers {
		if !isXAIName(provider.ID) && !isXAIName(provider.RouteModel) && !isXAIName(provider.UpstreamModel) {
			continue
		}
		if _, exists := providerIDs[provider.ID]; exists {
			continue
		}
		providerIDs[provider.ID] = struct{}{}
		usage.TodayTokens += provider.TokensToday
		usage.DailyTokenLimit += provider.DailyTokenLimit
	}

	found := false
	for _, bucket := range summary.ByProvider {
		if _, ok := providerIDs[bucket.Name]; !ok {
			continue
		}
		addRouterUsage(&usage, bucket)
		found = true
	}
	if found || len(providerIDs) > 0 {
		return usage, true
	}

	for _, bucket := range summary.ByModel {
		if !isXAIName(bucket.Name) {
			continue
		}
		addRouterUsage(&usage, bucket)
		found = true
	}
	return usage, found
}

func addRouterUsage(usage *XAIUsage, bucket routerUsageBucket) {
	usage.Requests += bucket.Requests
	usage.Successes += bucket.Successes
	usage.Errors += bucket.Errors
	usage.InputTokens += bucket.InputTokens
	usage.OutputTokens += bucket.OutputTokens
	usage.TotalTokens += bucket.TotalTokens
	if laterTimestamp(bucket.LastRequestAt, usage.LastRequestAt) {
		usage.LastRequestAt = bucket.LastRequestAt
	}
}

func isXAIName(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "grok") || strings.Contains(lower, "xai")
}

func laterTimestamp(candidate, current string) bool {
	if candidate == "" {
		return false
	}
	if current == "" {
		return true
	}
	candidateTime, candidateErr := time.Parse(time.RFC3339, candidate)
	currentTime, currentErr := time.Parse(time.RFC3339, current)
	if candidateErr == nil && currentErr == nil {
		return candidateTime.After(currentTime)
	}
	return candidate > current
}

func publicRouterUsageError(err error) string {
	message := err.Error()
	if strings.Contains(message, "401") || strings.Contains(message, "403") || strings.Contains(message, "API key") {
		return "CPA 路由用量授权不可用"
	}
	return "CPA 暂时无法读取 xAI 路由统计"
}

func publicXAIOfficialUsageError(err error) string {
	message := err.Error()
	if strings.Contains(message, "401") || strings.Contains(message, "403") {
		return "xAI 官方账单授权不可用"
	}
	if strings.Contains(message, "404") {
		return "xAI 官方团队配置无效"
	}
	return "xAI 官方账单暂时不可用"
}
