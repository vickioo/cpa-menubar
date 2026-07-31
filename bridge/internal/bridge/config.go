package bridge

import (
	"errors"
	"os"
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
