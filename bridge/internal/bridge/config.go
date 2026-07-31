package bridge

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen               string
	AuthDir              string
	DesktopToken         string
	DesktopTokenFile     string
	CLIProxyURL          string
	RouterURL            string
	RouterAPIKey         string
	RouterAPIKeyFile     string
	XAIManagementURL     string
	XAIManagementAPIKey  string
	XAIManagementTeamID  string
	XAIManagementKeyFile string
	UsageTimeout         time.Duration
	OAuthTimeout         time.Duration
	AccountSource        string
	Sub2DatabaseURL      string
	Sub2ProbeDatabaseURL string
	Sub2ProbeCooldown    time.Duration
	Sub2ProbeConcurrency int
	Sub2Target0703ID     int
	Sub2TargetFuIDs      []string
}

func LoadConfig() (Config, error) {
	cfg := Config{
		Listen:               envOr("CPA_DESKTOP_LISTEN", "127.0.0.1:8330"),
		AuthDir:              envOr("CPA_AUTH_DIR", "/var/lib/cliproxy/auth"),
		DesktopToken:         strings.TrimSpace(os.Getenv("CPA_DESKTOP_TOKEN")),
		DesktopTokenFile:     envOr("CPA_DESKTOP_TOKEN_FILE", "/etc/cpa-desktop-bridge.token"),
		CLIProxyURL:          envOr("CPA_CLIPROXY_URL", "http://127.0.0.1:8317"),
		RouterURL:            envOr("CPA_ROUTER_URL", "http://127.0.0.1:8327"),
		RouterAPIKey:         strings.TrimSpace(os.Getenv("CPA_ROUTER_API_KEY")),
		RouterAPIKeyFile:     envOr("CPA_ROUTER_API_KEY_FILE", "/etc/cpa-desktop-bridge/router.env"),
		XAIManagementURL:     envOr("XAI_MANAGEMENT_URL", "https://management-api.x.ai"),
		XAIManagementAPIKey:  strings.TrimSpace(os.Getenv("XAI_MANAGEMENT_API_KEY")),
		XAIManagementTeamID:  strings.TrimSpace(os.Getenv("XAI_TEAM_ID")),
		XAIManagementKeyFile: envOr("XAI_MANAGEMENT_API_KEY_FILE", "/etc/cpa-desktop-bridge/xai-management.env"),
		UsageTimeout:         15 * time.Second,
		OAuthTimeout:         5 * time.Minute,
		AccountSource:        strings.ToLower(envOr("CPA_ACCOUNT_SOURCE", "files")),
		Sub2DatabaseURL:      strings.TrimSpace(os.Getenv("SUB2_DATABASE_URL")),
		Sub2ProbeDatabaseURL: strings.TrimSpace(os.Getenv("SUB2_PROBE_DATABASE_URL")),
		Sub2ProbeCooldown:    time.Duration(envIntOr("SUB2_PROBE_COOLDOWN_SECONDS", 45)) * time.Second,
		Sub2ProbeConcurrency: envIntOr("SUB2_PROBE_CONCURRENCY", 3),
		Sub2Target0703ID:     envIntOr("SUB2_TARGET_0703_ACCOUNT_ID", 20),
		Sub2TargetFuIDs:      splitCSV(envOr("SUB2_TARGET_FU_ACCOUNT_IDS", "2,24")),
	}

	if cfg.DesktopToken == "" && cfg.DesktopTokenFile != "" {
		data, err := os.ReadFile(cfg.DesktopTokenFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
		cfg.DesktopToken = strings.TrimSpace(string(data))
	}
	if len(cfg.DesktopToken) < 32 {
		return Config{}, errors.New("CPA desktop token must contain at least 32 characters")
	}
	if cfg.AccountSource != "files" && cfg.AccountSource != "sub2" {
		return Config{}, errors.New("CPA_ACCOUNT_SOURCE must be files or sub2")
	}
	if cfg.AccountSource == "sub2" && cfg.Sub2DatabaseURL == "" {
		return Config{}, errors.New("SUB2_DATABASE_URL is required when CPA_ACCOUNT_SOURCE=sub2")
	}
	if cfg.Sub2ProbeCooldown < 30*time.Second {
		cfg.Sub2ProbeCooldown = 30 * time.Second
	}
	if cfg.Sub2ProbeConcurrency < 1 || cfg.Sub2ProbeConcurrency > 6 {
		cfg.Sub2ProbeConcurrency = 3
	}
	if cfg.RouterAPIKey == "" && cfg.RouterAPIKeyFile != "" {
		value, err := readEnvFileValue(cfg.RouterAPIKeyFile, "CPA_SMART_ROUTER_KEY")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
		cfg.RouterAPIKey = value
	}
	if cfg.XAIManagementKeyFile != "" && (cfg.XAIManagementAPIKey == "" || cfg.XAIManagementTeamID == "") {
		if cfg.XAIManagementAPIKey == "" {
			value, err := readEnvFileValue(cfg.XAIManagementKeyFile, "XAI_MANAGEMENT_API_KEY")
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return Config{}, err
			}
			cfg.XAIManagementAPIKey = value
		}
		if cfg.XAIManagementTeamID == "" {
			value, err := readEnvFileValue(cfg.XAIManagementKeyFile, "XAI_TEAM_ID")
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return Config{}, err
			}
			cfg.XAIManagementTeamID = value
		}
	}
	return cfg, nil
}

func envIntOr(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitCSV(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func readEnvFileValue(path, name string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != name {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`), nil
	}
	return "", nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
