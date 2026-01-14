package config

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mockGitHubClient is a mock implementation of GitHubClient for testing.
type mockGitHubClient struct {
	makeRequestFunc func(ctx context.Context, method, apiURL string, body any) (*http.Response, error)
	currentOrg      string
}

func (m *mockGitHubClient) SetCurrentOrg(org string) {
	m.currentOrg = org
}

func (m *mockGitHubClient) MakeRequest(ctx context.Context, method, apiURL string, body any) (*http.Response, error) {
	if m.makeRequestFunc != nil {
		return m.makeRequestFunc(ctx, method, apiURL, body)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
	if cfg.PendingTestGrace != DefaultPendingTestGrace {
		t.Errorf("PendingTestGrace = %d, want %d", cfg.PendingTestGrace, DefaultPendingTestGrace)
	}
	if cfg.FailingTestGrace != DefaultFailingTestGrace {
		t.Errorf("FailingTestGrace = %d, want %d", cfg.FailingTestGrace, DefaultFailingTestGrace)
	}
	if cfg.MaxReviewers != DefaultMaxReviewers {
		t.Errorf("MaxReviewers = %d, want %d", cfg.MaxReviewers, DefaultMaxReviewers)
	}
	if cfg.MaxPRsPerReviewer != DefaultMaxPRsPerReviewer {
		t.Errorf("MaxPRsPerReviewer = %d, want %d", cfg.MaxPRsPerReviewer, DefaultMaxPRsPerReviewer)
	}
	if cfg.MinAge != DefaultMinAge {
		t.Errorf("MinAge = %d, want %d", cfg.MinAge, DefaultMinAge)
	}
	if cfg.MaxAge != DefaultMaxAge {
		t.Errorf("MaxAge = %d, want %d", cfg.MaxAge, DefaultMaxAge)
	}
}

func TestNewManager(t *testing.T) {
	client := &mockGitHubClient{}
	manager := NewManager(client)

	if manager == nil {
		t.Fatal("NewManager returned nil")
	}
	if manager.client != client {
		t.Error("manager.client not set correctly")
	}
	if manager.cache == nil {
		t.Error("manager.cache is nil")
	}
}

func TestManager_Config_UsesCache(t *testing.T) {
	requestCount := 0
	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			requestCount++
			// Return 404 to use defaults (still gets cached)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	manager := NewManager(client)
	ctx := context.Background()

	// First call should load from GitHub
	cfg1 := manager.Config(ctx, "test-org")
	if cfg1 == nil {
		t.Fatal("Config returned nil")
	}
	if requestCount != 1 {
		t.Errorf("requestCount = %d after first call, want 1", requestCount)
	}

	// Second call should use cache (no additional request)
	cfg2 := manager.Config(ctx, "test-org")
	if cfg2 == nil {
		t.Fatal("Second Config returned nil")
	}
	if requestCount != 1 {
		t.Errorf("requestCount = %d after cached call, want 1", requestCount)
	}
}

func TestManager_InvalidateConfig(t *testing.T) {
	requestCount := 0
	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			requestCount++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	manager := NewManager(client)
	ctx := context.Background()

	// First call
	manager.Config(ctx, "test-org")
	if requestCount != 1 {
		t.Errorf("requestCount = %d after first call, want 1", requestCount)
	}

	// Second call (cached)
	manager.Config(ctx, "test-org")
	if requestCount != 1 {
		t.Errorf("requestCount = %d after cached call, want 1", requestCount)
	}

	// Invalidate cache
	manager.InvalidateConfig("test-org")

	// Third call (should fetch again)
	manager.Config(ctx, "test-org")
	if requestCount != 2 {
		t.Errorf("requestCount = %d after invalidation, want 2", requestCount)
	}
}

func TestManager_Config_ParsesYAML(t *testing.T) {
	yamlContent := `
min_grace_period: 5
pending_test_grace: 30
failing_test_grace: 120
max_reviewers: 3
max_prs_per_reviewer: 5
excluded_users:
  - bot-account
  - service-account
excluded_paths:
  - "vendor/**"
min_age: 1
max_age: 720
`
	encodedContent := base64.StdEncoding.EncodeToString([]byte(yamlContent))
	responseBody, err := json.Marshal(map[string]string{
		"content":  encodedContent,
		"encoding": "base64",
	})
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(responseBody))),
			}, nil
		},
	}

	manager := NewManager(client)
	ctx := context.Background()

	cfg := manager.Config(ctx, "test-org")

	if cfg.MinGracePeriod != 5 {
		t.Errorf("MinGracePeriod = %d, want 5", cfg.MinGracePeriod)
	}
	if cfg.PendingTestGrace != 30 {
		t.Errorf("PendingTestGrace = %d, want 30", cfg.PendingTestGrace)
	}
	if cfg.FailingTestGrace != 120 {
		t.Errorf("FailingTestGrace = %d, want 120", cfg.FailingTestGrace)
	}
	if cfg.MaxReviewers != 3 {
		t.Errorf("MaxReviewers = %d, want 3", cfg.MaxReviewers)
	}
	if cfg.MaxPRsPerReviewer != 5 {
		t.Errorf("MaxPRsPerReviewer = %d, want 5", cfg.MaxPRsPerReviewer)
	}
	if len(cfg.ExcludedUsers) != 2 {
		t.Errorf("len(ExcludedUsers) = %d, want 2", len(cfg.ExcludedUsers))
	}
	if len(cfg.ExcludedPaths) != 1 {
		t.Errorf("len(ExcludedPaths) = %d, want 1", len(cfg.ExcludedPaths))
	}
	if cfg.MinAge != 1 {
		t.Errorf("MinAge = %d, want 1", cfg.MinAge)
	}
	if cfg.MaxAge != 720 {
		t.Errorf("MaxAge = %d, want 720", cfg.MaxAge)
	}
}

func TestManager_Config_PartialYAML(t *testing.T) {
	// Only specify some values, rest should use defaults
	yamlContent := `
max_reviewers: 4
excluded_users:
  - test-bot
`
	encodedContent := base64.StdEncoding.EncodeToString([]byte(yamlContent))
	responseBody, err := json.Marshal(map[string]string{
		"content":  encodedContent,
		"encoding": "base64",
	})
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(responseBody))),
			}, nil
		},
	}

	manager := NewManager(client)
	ctx := context.Background()

	cfg := manager.Config(ctx, "test-org")

	// Specified value
	if cfg.MaxReviewers != 4 {
		t.Errorf("MaxReviewers = %d, want 4", cfg.MaxReviewers)
	}

	// Default values for unspecified fields
	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want default %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
	if cfg.PendingTestGrace != DefaultPendingTestGrace {
		t.Errorf("PendingTestGrace = %d, want default %d", cfg.PendingTestGrace, DefaultPendingTestGrace)
	}
	if cfg.MaxAge != DefaultMaxAge {
		t.Errorf("MaxAge = %d, want default %d", cfg.MaxAge, DefaultMaxAge)
	}
}

func TestManager_Config_NotFound(t *testing.T) {
	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	manager := NewManager(client)
	ctx := context.Background()

	cfg := manager.Config(ctx, "test-org")

	// Should return defaults when config not found
	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want default %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
	if cfg.MaxReviewers != DefaultMaxReviewers {
		t.Errorf("MaxReviewers = %d, want default %d", cfg.MaxReviewers, DefaultMaxReviewers)
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &OrgConfig{
		// Leave all at zero
	}

	applyDefaults(cfg)

	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
	if cfg.PendingTestGrace != DefaultPendingTestGrace {
		t.Errorf("PendingTestGrace = %d, want %d", cfg.PendingTestGrace, DefaultPendingTestGrace)
	}
	if cfg.FailingTestGrace != DefaultFailingTestGrace {
		t.Errorf("FailingTestGrace = %d, want %d", cfg.FailingTestGrace, DefaultFailingTestGrace)
	}
	if cfg.MaxReviewers != DefaultMaxReviewers {
		t.Errorf("MaxReviewers = %d, want %d", cfg.MaxReviewers, DefaultMaxReviewers)
	}
	if cfg.MaxPRsPerReviewer != DefaultMaxPRsPerReviewer {
		t.Errorf("MaxPRsPerReviewer = %d, want %d", cfg.MaxPRsPerReviewer, DefaultMaxPRsPerReviewer)
	}
	if cfg.MaxAge != DefaultMaxAge {
		t.Errorf("MaxAge = %d, want %d", cfg.MaxAge, DefaultMaxAge)
	}
}

func TestApplyDefaults_PreservesSetValues(t *testing.T) {
	cfg := &OrgConfig{
		MinGracePeriod:    10,
		MaxReviewers:      5,
		MaxPRsPerReviewer: 15,
	}

	applyDefaults(cfg)

	// Values that were set should be preserved
	if cfg.MinGracePeriod != 10 {
		t.Errorf("MinGracePeriod = %d, want 10", cfg.MinGracePeriod)
	}
	if cfg.MaxReviewers != 5 {
		t.Errorf("MaxReviewers = %d, want 5", cfg.MaxReviewers)
	}
	if cfg.MaxPRsPerReviewer != 15 {
		t.Errorf("MaxPRsPerReviewer = %d, want 15", cfg.MaxPRsPerReviewer)
	}

	// Values that weren't set should get defaults
	if cfg.PendingTestGrace != DefaultPendingTestGrace {
		t.Errorf("PendingTestGrace = %d, want %d", cfg.PendingTestGrace, DefaultPendingTestGrace)
	}
	if cfg.MaxAge != DefaultMaxAge {
		t.Errorf("MaxAge = %d, want %d", cfg.MaxAge, DefaultMaxAge)
	}
}

func TestManager_Config_RequestError(t *testing.T) {
	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return nil, io.ErrUnexpectedEOF
		},
	}

	manager := NewManager(client)
	// Use short timeout to avoid retry delays in tests
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return defaults when request fails
	cfg := manager.Config(ctx, "test-org")
	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want default %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
}

func TestManager_Config_Non200Status(t *testing.T) {
	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("server error")),
			}, nil
		},
	}

	manager := NewManager(client)
	// Use short timeout to avoid retry delays in tests
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return defaults when server returns 500
	cfg := manager.Config(ctx, "test-org")
	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want default %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
}

func TestManager_Config_InvalidJSON(t *testing.T) {
	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not valid json")),
			}, nil
		},
	}

	manager := NewManager(client)
	// Use short timeout to avoid retry delays in tests
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return defaults when JSON is invalid
	cfg := manager.Config(ctx, "test-org")
	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want default %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
}

func TestManager_Config_UnexpectedEncoding(t *testing.T) {
	responseBody, err := json.Marshal(map[string]string{
		"content":  "some content",
		"encoding": "utf-8", // Not base64
	})
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(responseBody))),
			}, nil
		},
	}

	manager := NewManager(client)
	// Use short timeout to avoid retry delays in tests
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return defaults when encoding is not base64
	cfg := manager.Config(ctx, "test-org")
	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want default %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
}

func TestManager_Config_InvalidBase64(t *testing.T) {
	responseBody, err := json.Marshal(map[string]string{
		"content":  "not-valid-base64!!!", // Invalid base64
		"encoding": "base64",
	})
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(responseBody))),
			}, nil
		},
	}

	manager := NewManager(client)
	// Use short timeout to avoid retry delays in tests
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return defaults when base64 is invalid
	cfg := manager.Config(ctx, "test-org")
	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want default %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
}

func TestManager_Config_InvalidYAML(t *testing.T) {
	// Invalid YAML - unclosed bracket
	yamlContent := "min_grace_period: [invalid yaml"
	encodedContent := base64.StdEncoding.EncodeToString([]byte(yamlContent))
	responseBody, err := json.Marshal(map[string]string{
		"content":  encodedContent,
		"encoding": "base64",
	})
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(responseBody))),
			}, nil
		},
	}

	manager := NewManager(client)
	ctx := context.Background()

	// Should return defaults when YAML is invalid (no retry needed, parse error)
	cfg := manager.Config(ctx, "test-org")
	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want default %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
}

type errorReader struct{}

func (e errorReader) Read(_ []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func (e errorReader) Close() error {
	return nil
}

func TestManager_Config_ReadBodyError(t *testing.T) {
	client := &mockGitHubClient{
		makeRequestFunc: func(_ context.Context, _, _ string, _ any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       errorReader{},
			}, nil
		},
	}

	manager := NewManager(client)
	// Use short timeout to avoid retry delays in tests
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return defaults when body read fails
	cfg := manager.Config(ctx, "test-org")
	if cfg.MinGracePeriod != DefaultMinGracePeriod {
		t.Errorf("MinGracePeriod = %d, want default %d", cfg.MinGracePeriod, DefaultMinGracePeriod)
	}
}
