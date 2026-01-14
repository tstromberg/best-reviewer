package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

//go:embed templates/index.html
var indexTemplate string

const (
	maxURLLength       = 200
	rateLimitPerMinute = 10
	rateLimitBurst     = 3
)

// webRateLimiter provides per-IP rate limiting for the web frontend.
type webRateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
}

func newWebRateLimiter() *webRateLimiter {
	return &webRateLimiter{
		limiters: make(map[string]*rate.Limiter),
	}
}

func (r *webRateLimiter) allow(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.limiters[ip]; !ok {
		// ~10 requests per minute with burst of 3
		r.limiters[ip] = rate.NewLimiter(rate.Every(time.Minute/rateLimitPerMinute), rateLimitBurst)
	}

	// Cleanup old limiters periodically to prevent memory leak
	if len(r.limiters) > 10000 {
		n := 0
		for k := range r.limiters {
			delete(r.limiters, k)
			n++
			if n >= 5000 {
				break
			}
		}
	}

	return r.limiters[ip].Allow()
}

// AnalyzeRequest is the JSON request body for PR analysis.
type AnalyzeRequest struct {
	URL string `json:"url"`
}

// AnalyzeResponse is the JSON response for PR analysis.
//
//nolint:govet // fieldalignment: struct field order optimized for readability
type AnalyzeResponse struct {
	PR         PRSummary         `json:"pr,omitzero"`
	Candidates []CandidateResult `json:"candidates,omitempty"`
	Debug      DebugInfo         `json:"debug,omitzero"`
	Error      string            `json:"error,omitempty"`
}

// PRSummary contains basic PR information.
//
//nolint:govet // fieldalignment: struct field order optimized for readability
type PRSummary struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	Repository   string `json:"repository"`
	Owner        string `json:"owner"`
	FilesChanged int    `json:"files_changed"`
	State        string `json:"state"`
}

// CandidateResult contains reviewer candidate information.
//
//nolint:govet // fieldalignment: struct field order optimized for readability
type CandidateResult struct {
	Username        string         `json:"username"`
	FinalScore      int            `json:"final_score"`
	ScoreBreakdown  map[string]int `json:"score_breakdown"`
	SelectionMethod string         `json:"selection_method"`
}

// DebugInfo contains debug information about the analysis.
type DebugInfo struct {
	FilesAnalyzed    int   `json:"files_analyzed"`
	CandidatesFound  int   `json:"candidates_found"`
	ProcessingTimeMs int64 `json:"processing_time_ms"`
}

// parsePRURLForWeb extracts owner, repo, and PR number from a GitHub PR URL.
// Returns an error if the URL is invalid or not from github.com.
//
//nolint:revive // function-result-limit: 4 returns is clearer than a struct for this use case
func parsePRURLForWeb(prURL string) (owner, repo string, number int, err error) {
	if len(prURL) > maxURLLength {
		return "", "", 0, errors.New("URL too long")
	}

	// Remove protocol prefix
	prURL = strings.TrimPrefix(prURL, "https://")
	prURL = strings.TrimPrefix(prURL, "http://")

	// Must be from github.com
	if !strings.HasPrefix(prURL, "github.com/") {
		return "", "", 0, errors.New("URL must be from github.com")
	}
	prURL = strings.TrimPrefix(prURL, "github.com/")

	// Split by /
	parts := strings.Split(prURL, "/")
	if len(parts) < 4 || parts[2] != "pull" {
		return "", "", 0, errors.New("expected format: https://github.com/owner/repo/pull/123")
	}

	// Parse PR number (strip any query params or fragments)
	numStr := strings.Split(parts[3], "?")[0]
	numStr = strings.Split(numStr, "#")[0]

	number, err = strconv.Atoi(numStr)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid PR number: %w", err)
	}

	return parts[0], parts[1], number, nil
}

// isPublicRepo checks if a repository is publicly accessible using an unauthenticated request.
func isPublicRepo(ctx context.Context, owner, repo string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // Best effort drain
		_ = resp.Body.Close()                 //nolint:errcheck // Best effort close
	}()

	// Public repos return 200, private/nonexistent return 404/403
	return resp.StatusCode == http.StatusOK
}

// securityHeaders wraps a handler with security headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' https: data:; "+
				"connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// handleWebPage serves the web frontend HTML page.
func (*Bot) handleWebPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300") // Cache for 5 minutes
	if _, err := w.Write([]byte(indexTemplate)); err != nil {
		slog.Warn("Failed to write web page", "error", err)
	}
}

// handleAnalyze handles PR analysis requests.
func (b *Bot) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	w.Header().Set("Content-Type", "application/json")

	// Extract client IP for rate limiting (X-Forwarded-For is set by Cloud Run)
	ip := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			ip = strings.TrimSpace(xff[:idx])
		} else {
			ip = strings.TrimSpace(xff)
		}
	} else if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}

	if !b.webLimiter.allow(ip) {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(w, AnalyzeResponse{Error: "Rate limit exceeded. Please try again later."})
		return
	}

	// Parse request
	var req AnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, AnalyzeResponse{Error: "Invalid request body"})
		return
	}

	// Parse and validate URL
	owner, repo, prNumber, err := parsePRURLForWeb(req.URL)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, AnalyzeResponse{Error: err.Error()})
		return
	}

	// Check if repo is public (security: only allow public repos)
	if !isPublicRepo(ctx, owner, repo) {
		w.WriteHeader(http.StatusForbidden)
		writeJSON(w, AnalyzeResponse{Error: "Repository not accessible. Only public repositories are supported."})
		return
	}

	// Fetch PR details
	pr, err := b.client.PullRequest(ctx, owner, repo, prNumber)
	if err != nil {
		slog.Warn("Failed to fetch PR", "owner", owner, "repo", repo, "pr", prNumber, "error", err)
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, AnalyzeResponse{Error: "Pull request not found or not accessible"})
		return
	}

	// Find reviewers
	candidates, err := b.finder.Find(ctx, pr)
	if err != nil {
		slog.Error("Failed to find reviewers", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, AnalyzeResponse{Error: "Failed to analyze reviewers"})
		return
	}

	// Build response
	resp := AnalyzeResponse{
		PR: PRSummary{
			Number:       pr.Number,
			Title:        pr.Title,
			Author:       pr.Author,
			Repository:   pr.Repository,
			Owner:        pr.Owner,
			FilesChanged: len(pr.ChangedFiles),
			State:        pr.State,
		},
		Candidates: make([]CandidateResult, 0, len(candidates)),
		Debug: DebugInfo{
			FilesAnalyzed:    len(pr.ChangedFiles),
			CandidatesFound:  len(candidates),
			ProcessingTimeMs: time.Since(startTime).Milliseconds(),
		},
	}

	for _, c := range candidates {
		resp.Candidates = append(resp.Candidates, CandidateResult{
			Username:        c.Username,
			FinalScore:      c.ContextScore,
			ScoreBreakdown:  c.SourceScores,
			SelectionMethod: c.SelectionMethod,
		})
	}

	slog.Info("Web analysis complete",
		"owner", owner, "repo", repo, "pr", prNumber,
		"candidates", len(candidates),
		"duration_ms", resp.Debug.ProcessingTimeMs)

	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		slog.Warn("Failed to encode JSON response", "error", err)
	}
}
