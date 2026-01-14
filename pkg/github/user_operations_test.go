package github

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/codeGROOVE-dev/best-reviewer/pkg/types"
	"github.com/codeGROOVE-dev/fido"
	"github.com/codeGROOVE-dev/fido/pkg/store/null"
)

// mustNewCache creates a cache for testing using an in-memory null store.
func mustNewCache(t *testing.T) *fido.TieredCache[string, any] {
	t.Helper()
	cache, err := fido.NewTiered(null.New[string, any](), fido.TTL(time.Hour))
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	return cache
}

func TestClient_HasWriteAccess_CachedCollaborators(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	// Cache collaborators list
	cacheKey := makeCacheKey("collaborators", "owner", "repo")
	if err := c.cache.Set(ctx, cacheKey, []string{"alice", "bob", "charlie"}); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	tests := []struct {
		username string
		want     bool
	}{
		{"alice", true},
		{"bob", true},
		{"charlie", true},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			got := c.HasWriteAccess(ctx, "owner", "repo", tt.username)
			if got != tt.want {
				t.Errorf("HasWriteAccess(%q) = %v, want %v", tt.username, got, tt.want)
			}
		})
	}
}

func TestClient_HasWriteAccess_PermissionDenied(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	// Cache permission denied flag
	permCacheKey := makeCacheKey("collaborators-permission", "owner", "repo")
	if err := c.cache.Set(ctx, permCacheKey, true); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Should fail-open (return true) when we don't have permission to check
	got := c.HasWriteAccess(ctx, "owner", "repo", "anyone")
	if !got {
		t.Error("HasWriteAccess should fail-open (return true) when permission denied")
	}
}

func TestClient_HasWriteAccess_NoCache(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	// No cached data - should fail-open
	got := c.HasWriteAccess(context.Background(), "owner", "repo", "anyone")
	if !got {
		t.Error("HasWriteAccess should fail-open (return true) when no cache available")
	}
}

func TestClient_cachedPR_Hit(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	expectedPR := &types.PullRequest{
		Owner:      "owner",
		Repository: "repo",
		Number:     123,
		Title:      "Test PR",
		UpdatedAt:  time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	// Cache the PR
	cacheKey := makeCacheKey("pr", "owner", "repo", "123")
	if err := c.cache.Set(ctx, cacheKey, expectedPR); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Test cache hit with matching updated_at
	updatedAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	pr, found := c.cachedPR(ctx, "owner", "repo", 123, &updatedAt)

	if !found {
		t.Fatal("expected cache hit")
	}

	if pr.Title != "Test PR" {
		t.Errorf("expected PR title 'Test PR', got %q", pr.Title)
	}
}

func TestClient_cachedPR_InvalidatedByUpdate(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	oldTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	pr := &types.PullRequest{
		Owner:      "owner",
		Repository: "repo",
		Number:     123,
		Title:      "Test PR",
		UpdatedAt:  oldTime,
	}

	// Cache the PR with old timestamp
	cacheKey := makeCacheKey("pr", "owner", "repo", "123")
	if err := c.cache.Set(ctx, cacheKey, pr); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Try to retrieve with newer timestamp - should invalidate cache
	newTime := time.Date(2025, 1, 1, 13, 0, 0, 0, time.UTC)
	_, found := c.cachedPR(ctx, "owner", "repo", 123, &newTime)

	if found {
		t.Error("cache should be invalidated when updated_at changed")
	}
}

func TestClient_cachedPR_NoExpectedTime(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	pr := &types.PullRequest{
		Owner:      "owner",
		Repository: "repo",
		Number:     123,
		Title:      "Test PR",
		UpdatedAt:  time.Now(),
	}

	cacheKey := makeCacheKey("pr", "owner", "repo", "123")
	if err := c.cache.Set(ctx, cacheKey, pr); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Retrieve without expected time - should return cached value
	cached, found := c.cachedPR(ctx, "owner", "repo", 123, nil)
	if !found {
		t.Fatal("expected cache hit")
	}

	if cached.Title != "Test PR" {
		t.Errorf("expected cached PR title 'Test PR', got %q", cached.Title)
	}
}

func TestClient_cachedPR_Miss(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	// No cached PR
	_, found := c.cachedPR(ctx, "owner", "repo", 999, nil)
	if found {
		t.Error("expected cache miss for non-existent PR")
	}
}

func TestClient_cachePR(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	pr := &types.PullRequest{
		Owner:      "owner",
		Repository: "repo",
		Number:     123,
		Title:      "Test PR",
	}

	c.cachePR(ctx, pr)

	// Verify it was cached
	cacheKey := makeCacheKey("pr", "owner", "repo", "123")
	cached, found, err := c.cache.Get(ctx, cacheKey)
	if err != nil {
		t.Fatalf("failed to get cache: %v", err)
	}
	if !found {
		t.Fatal("PR should be cached")
	}

	cachedPR, ok := cached.(*types.PullRequest)
	if !ok {
		t.Fatal("cached value should be *types.PullRequest")
	}

	if cachedPR.Title != "Test PR" {
		t.Errorf("expected cached title 'Test PR', got %q", cachedPR.Title)
	}
}

func TestClient_OpenPRCount_Cached(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	// Pre-cache a PR count
	cacheKey := makeCacheKey("pr-count", "myorg", "alice")
	if err := c.cache.Set(ctx, cacheKey, 5); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	count, err := c.OpenPRCount(ctx, "myorg", "alice", time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count != 5 {
		t.Errorf("expected cached count 5, got %d", count)
	}
}

func TestClient_OpenPRCount_CachedFailure(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	// Cache a failure
	failureKey := makeCacheKey("pr-count-failure", "myorg", "alice")
	if err := c.cache.Set(ctx, failureKey, true); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	_, err := c.OpenPRCount(ctx, "myorg", "alice", time.Hour)
	if err == nil {
		t.Error("expected error for cached failure")
	}

	if err.Error() != "recently failed to get PR count (cached failure)" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClient_OpenPRCount_InvalidParams(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	tests := []struct {
		name string
		org  string
		user string
	}{
		{"empty org", "", "alice"},
		{"empty user", "myorg", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.OpenPRCount(context.Background(), tt.org, tt.user, time.Hour)
			if err == nil {
				t.Error("expected error for invalid params")
			}
		})
	}
}

func TestClient_BatchOpenPRCount_AllCached(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	ctx := context.Background()

	// Pre-cache PR counts for all users
	for _, tc := range []struct {
		user  string
		count int
	}{
		{"alice", 3},
		{"bob", 5},
		{"charlie", 1},
	} {
		if err := c.cache.Set(ctx, makeCacheKey("pr-count", "myorg", tc.user), tc.count); err != nil {
			t.Fatalf("failed to set cache for %s: %v", tc.user, err)
		}
	}

	result, err := c.BatchOpenPRCount(ctx, "myorg", []string{"alice", "bob", "charlie"}, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]int{
		"alice":   3,
		"bob":     5,
		"charlie": 1,
	}

	for user, count := range expected {
		if result[user] != count {
			t.Errorf("expected count %d for %s, got %d", count, user, result[user])
		}
	}
}

func TestClient_BatchOpenPRCount_EmptyUsers(t *testing.T) {
	c := &Client{
		cache: mustNewCache(t),
	}

	result, err := c.BatchOpenPRCount(context.Background(), "myorg", []string{}, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty result for empty users list, got %v", result)
	}
}

func TestMakeCacheKey_Consistency(t *testing.T) {
	// Verify cache keys are consistent and don't conflict
	key1 := makeCacheKey("pr", "owner", "repo", strconv.Itoa(123))
	key2 := makeCacheKey("pr", "owner", "repo", "123")

	if key1 != key2 {
		t.Errorf("cache keys should be identical: %q != %q", key1, key2)
	}

	// Verify different keys don't collide
	prKey := makeCacheKey("pr", "owner", "repo", "1")
	countKey := makeCacheKey("pr-count", "owner", "user1")

	if prKey == countKey {
		t.Error("different cache key types should not collide")
	}
}
