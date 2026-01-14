package config

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/codeGROOVE-dev/fido"
	"github.com/codeGROOVE-dev/retry"
	"gopkg.in/yaml.v3"
)

// OrgConfig represents the assigner.yaml configuration for a GitHub org.
type OrgConfig struct {
	// Filtering
	ExcludedUsers []string `yaml:"excluded_users"`
	ExcludedPaths []string `yaml:"excluded_paths"`

	// Grace periods (in minutes)
	MinGracePeriod   int `yaml:"min_grace_period"`
	PendingTestGrace int `yaml:"pending_test_grace"`
	FailingTestGrace int `yaml:"failing_test_grace"`

	// Reviewer assignment
	MaxReviewers      int `yaml:"max_reviewers"`
	MaxPRsPerReviewer int `yaml:"max_prs_per_reviewer"`

	// PR age constraints (in hours)
	MinAge int `yaml:"min_age"`
	MaxAge int `yaml:"max_age"`
}

// GitHubClient defines the GitHub API operations needed for config loading.
type GitHubClient interface {
	SetCurrentOrg(org string)
	MakeRequest(ctx context.Context, method, apiURL string, body any) (*http.Response, error)
}

// Manager manages per-organization configurations.
type Manager struct {
	client GitHubClient
	cache  *fido.Cache[string, *OrgConfig]
}

// NewManager creates a new config manager.
func NewManager(client GitHubClient) *Manager {
	return &Manager{
		client: client,
		cache:  fido.New[string, *OrgConfig](fido.TTL(ConfigCacheTTL)),
	}
}

// DefaultConfig returns a configuration with all default values.
func DefaultConfig() *OrgConfig {
	return &OrgConfig{
		MinGracePeriod:    DefaultMinGracePeriod,
		PendingTestGrace:  DefaultPendingTestGrace,
		FailingTestGrace:  DefaultFailingTestGrace,
		MaxReviewers:      DefaultMaxReviewers,
		MaxPRsPerReviewer: DefaultMaxPRsPerReviewer,
		ExcludedUsers:     nil,
		ExcludedPaths:     nil,
		MinAge:            DefaultMinAge,
		MaxAge:            DefaultMaxAge,
	}
}

// Config returns the configuration for an organization, loading it if necessary.
// Uses fido.FetchTTL to prevent thundering herd when multiple goroutines
// request the same org's config simultaneously.
func (m *Manager) Config(ctx context.Context, org string) *OrgConfig {
	cfg, err := m.cache.FetchTTL(org, ConfigCacheTTL, func() (*OrgConfig, error) {
		return m.loadConfig(ctx, org)
	})
	if err != nil {
		slog.Warn("Failed to load config, using defaults", "org", org, "error", err)
		return DefaultConfig()
	}
	return cfg
}

// InvalidateConfig removes a specific org's config from the cache.
func (m *Manager) InvalidateConfig(org string) {
	m.cache.Delete(org)
	slog.Info("Invalidated config cache", "org", org)
}

// errConfigNotFound is a sentinel error indicating config file doesn't exist.
var errConfigNotFound = errors.New("config not found")

// loadConfig fetches and parses the config file from GitHub.
// Returns DefaultConfig() when the config file doesn't exist (404).
func (m *Manager) loadConfig(ctx context.Context, org string) (*OrgConfig, error) {
	// Set the current org for authentication
	m.client.SetCurrentOrg(org)

	var content string
	var notFound bool
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", org, ConfigRepoName, ConfigFileName)

	slog.Info("Loading org config", "org", org, "repo", ConfigRepoName, "file", ConfigFileName)

	err := retry.Do(
		func() error {
			resp, reqErr := m.client.MakeRequest(ctx, "GET", apiURL, nil)
			if reqErr != nil {
				return reqErr
			}
			defer func() {
				if closeErr := resp.Body.Close(); closeErr != nil {
					slog.Warn("Failed to close response body", "error", closeErr)
				}
			}()

			if resp.StatusCode == http.StatusNotFound {
				slog.Info("Config file not found, will use defaults", "org", org)
				notFound = true
				return retry.Unrecoverable(errConfigNotFound)
			}

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("failed to fetch config (status %d)", resp.StatusCode)
			}

			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return fmt.Errorf("failed to read response: %w", readErr)
			}

			// GitHub API returns JSON with base64-encoded content
			var fileResp struct {
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
			if jsonErr := json.Unmarshal(body, &fileResp); jsonErr != nil {
				return fmt.Errorf("failed to parse response: %w", jsonErr)
			}

			if fileResp.Encoding != "base64" {
				return fmt.Errorf("unexpected encoding: %s", fileResp.Encoding)
			}

			decoded, decErr := base64.StdEncoding.DecodeString(fileResp.Content)
			if decErr != nil {
				return fmt.Errorf("failed to decode content: %w", decErr)
			}

			content = string(decoded)
			return nil
		},
		retry.Attempts(5),
		retry.Delay(time.Second),
		retry.MaxDelay(2*time.Minute),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(time.Second),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
	)

	// Return defaults for missing config (cacheable result)
	if notFound {
		return DefaultConfig(), nil
	}
	if err != nil {
		return nil, err
	}

	// Parse YAML
	cfg := DefaultConfig()
	if yamlErr := yaml.Unmarshal([]byte(content), cfg); yamlErr != nil {
		slog.Error("Failed to parse config YAML", "org", org, "error", yamlErr)
		return nil, fmt.Errorf("failed to parse YAML: %w", yamlErr)
	}

	// Apply defaults for any zero values
	applyDefaults(cfg)

	slog.Info("Loaded org config",
		"org", org,
		"min_grace_period", cfg.MinGracePeriod,
		"max_reviewers", cfg.MaxReviewers,
		"excluded_users", len(cfg.ExcludedUsers),
		"excluded_paths", len(cfg.ExcludedPaths))

	return cfg, nil
}

// applyDefaults fills in default values for any fields that weren't specified.
func applyDefaults(cfg *OrgConfig) {
	if cfg.MinGracePeriod == 0 {
		cfg.MinGracePeriod = DefaultMinGracePeriod
	}
	if cfg.PendingTestGrace == 0 {
		cfg.PendingTestGrace = DefaultPendingTestGrace
	}
	if cfg.FailingTestGrace == 0 {
		cfg.FailingTestGrace = DefaultFailingTestGrace
	}
	if cfg.MaxReviewers == 0 {
		cfg.MaxReviewers = DefaultMaxReviewers
	}
	if cfg.MaxPRsPerReviewer == 0 {
		cfg.MaxPRsPerReviewer = DefaultMaxPRsPerReviewer
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = DefaultMaxAge
	}
}
