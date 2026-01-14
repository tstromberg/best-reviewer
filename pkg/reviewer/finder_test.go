package reviewer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/codeGROOVE-dev/best-reviewer/pkg/internal/testutil"
	"github.com/codeGROOVE-dev/best-reviewer/pkg/types"
)

func TestNew(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	cfg := Config{
		PRCountCache: time.Hour,
	}

	finder := New(client, cfg)

	if finder == nil {
		t.Fatal("expected non-nil Finder")
	}

	if finder.client == nil {
		t.Error("expected non-nil client")
	}

	if finder.cache == nil {
		t.Error("expected non-nil cache")
	}

	if finder.prCountCache != time.Hour {
		t.Errorf("expected prCountCache to be 1 hour, got %v", finder.prCountCache)
	}
}

func TestFinder_Find_NilPR(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	_, err := finder.Find(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil PR")
	}
}

func TestFinder_Find_SinglePersonProject(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
	}

	// Configure only the PR author as collaborator (single-person project)
	client.SetCollaborators("test-owner", "test-repo", []string{"alice"})
	client.SetBotUser("alice", false)

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for single-person project, got %d", len(candidates))
	}
}

func TestFinder_Find_SmallTeam_OneMember(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
	}

	// Configure two collaborators: PR author and one other person
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate for small team, got %d", len(candidates))
	}

	if candidates[0].Username != "bob" {
		t.Errorf("expected candidate 'bob', got %q", candidates[0].Username)
	}

	if candidates[0].SelectionMethod != "small-team" {
		t.Errorf("expected selection method 'small-team', got %q", candidates[0].SelectionMethod)
	}

	if candidates[0].ContextScore != maxContextScore {
		t.Errorf("expected context score %d, got %d", maxContextScore, candidates[0].ContextScore)
	}
}

func TestFinder_Find_SmallTeam_TwoMembers(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
	}

	// Configure three collaborators: PR author and two others
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates for small team, got %d", len(candidates))
	}

	// Should return both bob and charlie
	usernames := make(map[string]bool)
	for _, c := range candidates {
		usernames[c.Username] = true
		if c.SelectionMethod != "small-team" {
			t.Errorf("expected selection method 'small-team', got %q", c.SelectionMethod)
		}
		if c.ContextScore != maxContextScore {
			t.Errorf("expected context score %d, got %d", maxContextScore, c.ContextScore)
		}
	}

	if !usernames["bob"] {
		t.Error("expected 'bob' in candidates")
	}
	if !usernames["charlie"] {
		t.Error("expected 'charlie' in candidates")
	}
	if usernames["alice"] {
		t.Error("PR author should not be in candidates")
	}
}

func TestFinder_Find_SmallTeam_ExcludeBots(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
	}

	// Configure collaborators including a bot
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "renovate-bot"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("renovate-bot", true)

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate (bot should be excluded), got %d", len(candidates))
	}

	if candidates[0].Username != "bob" {
		t.Errorf("expected candidate 'bob', got %q", candidates[0].Username)
	}
}

func TestFinder_checkSmallTeamProject_Cached(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
	}

	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)

	// First call - should hit API
	members1, count1, err := finder.checkSmallTeamProject(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count1 != 1 {
		t.Errorf("expected count 1, got %d", count1)
	}

	if len(members1) != 1 || members1[0] != "bob" {
		t.Errorf("expected members [bob], got %v", members1)
	}

	// Second call - should use cache
	members2, count2, err := finder.checkSmallTeamProject(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error on cached call: %v", err)
	}

	if count2 != count1 {
		t.Errorf("cached count mismatch: expected %d, got %d", count1, count2)
	}

	if len(members2) != len(members1) {
		t.Errorf("cached members length mismatch: expected %d, got %d", len(members1), len(members2))
	}
}

func TestFinder_checkSmallTeamProject_LargeTeam(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
	}

	// Configure large team (more than 2 valid members)
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)

	members, count, err := finder.checkSmallTeamProject(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return -1 to indicate no short-circuit needed
	if count != -1 {
		t.Errorf("expected count -1 for large team, got %d", count)
	}

	if len(members) != 0 {
		t.Errorf("expected empty members for large team, got %v", members)
	}
}

func TestFinder_isValidReviewer_Bot(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
	}

	client.SetBotUser("dependabot", true)

	valid := finder.isValidReviewer(ctx, pr, "dependabot")
	if valid {
		t.Error("expected bot to be invalid reviewer")
	}
}

func TestFinder_isValidReviewer_NoWriteAccess(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
	}

	client.SetBotUser("user1", false)
	client.SetWriteAccess("test-owner", "test-repo", "user1", false)

	valid := finder.isValidReviewer(ctx, pr, "user1")
	if valid {
		t.Error("expected user without write access to be invalid reviewer")
	}
}

func TestFinder_isValidReviewer_Valid(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
	}

	client.SetBotUser("user1", false)
	client.SetWriteAccess("test-owner", "test-repo", "user1", true)

	valid := finder.isValidReviewer(ctx, pr, "user1")
	if !valid {
		t.Error("expected valid user to be valid reviewer")
	}
}

func TestFinder_Find_LargeTeam_UsesOptimizedPath(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"bob"},
		ChangedFiles: []types.ChangedFile{
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team (more than 2 valid members) to trigger optimized path
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)

	// Set up write access and batch PR counts for the optimized path
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetWriteAccess("test-owner", "test-repo", "charlie", true)
	client.SetWriteAccess("test-owner", "test-repo", "dave", true)
	client.SetBatchOpenPRCount("test-owner", map[string]int{
		"bob":     1,
		"charlie": 2,
		"dave":    3,
	})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return candidates using the optimized algorithm
	// Bob is an assignee so should be included
	if len(candidates) == 0 {
		t.Error("expected at least one candidate from optimized path")
	}

	// Verify bob (assignee) is in the results
	found := false
	for _, c := range candidates {
		if c.Username == "bob" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected assignee 'bob' to be in candidates")
	}
}

func TestFinder_Find_CollaboratorsError(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"bob"},
		ChangedFiles: []types.ChangedFile{
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	// Set up collaborators error - this will cause checkSmallTeamProject to fail
	// The code should continue and fall through to findReviewersOptimized
	client.SetCollaboratorsError("test-owner", "test-repo", "permission denied")
	client.SetBotUser("bob", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still return candidates even if collaborators check fails
	// because the code continues to findReviewersOptimized
	if len(candidates) == 0 {
		t.Error("expected candidates even after collaborators error")
	}
}

func TestFinder_checkSmallTeamProject_CollaboratorsError(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
	}

	// Set collaborators error
	client.SetCollaboratorsError("test-owner", "test-repo", "permission denied")

	_, _, err := finder.checkSmallTeamProject(ctx, pr)
	if err == nil {
		t.Error("expected error from checkSmallTeamProject")
	}
}

// GraphQL query strings (must match exactly)
const dirCommitsQuery = `
	query($owner: String!, $repo: String!, $path: String!, $limit: Int!) {
		repository(owner: $owner, name: $repo) {
			defaultBranchRef {
				name
				target {
					... on Commit {
						history(first: $limit, path: $path) {
							nodes {
								oid
								author {
									user {
										login
									}
								}
								associatedPullRequests(first: 1) {
									nodes {
										number
										merged
										mergedAt
										author {
											login
										}
										mergedBy {
											login
										}
										reviews(first: 10, states: APPROVED) {
											nodes {
												author {
													login
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}`

const recentPRsQuery = `
	query($owner: String!, $repo: String!) {
		repository(owner: $owner, name: $repo) {
			pullRequests(first: 100, states: MERGED, orderBy: {field: CREATED_AT, direction: DESC}) {
				pageInfo {
					endCursor
					hasNextPage
				}
				nodes {
					number
					merged
					author {
						login
					}
					mergedAt
					mergedBy {
						login
					}
					reviews(first: 10, states: APPROVED) {
						nodes {
							author {
								login
							}
						}
					}
				}
			}
		}
	}`

func TestFinder_Find_WithDirectoryCommits(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		ChangedFiles: []types.ChangedFile{
			{Filename: "pkg/main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team to trigger optimized path
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave", "eve"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)
	client.SetBotUser("eve", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetWriteAccess("test-owner", "test-repo", "charlie", true)
	client.SetWriteAccess("test-owner", "test-repo", "dave", true)
	client.SetWriteAccess("test-owner", "test-repo", "eve", true)

	// Mock directory commits with PR data
	dirResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"name": "main",
					"target": map[string]any{
						"history": map[string]any{
							"nodes": []any{
								map[string]any{
									"oid": "abc123",
									"author": map[string]any{
										"user": map[string]any{"login": "bob"},
									},
									"associatedPullRequests": map[string]any{
										"nodes": []any{
											map[string]any{
												"number":   float64(50),
												"merged":   true,
												"mergedAt": "2024-01-01T12:00:00Z",
												"author":   map[string]any{"login": "bob"},
												"mergedBy": map[string]any{"login": "charlie"},
												"reviews": map[string]any{
													"nodes": []any{
														map[string]any{"author": map[string]any{"login": "dave"}},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(dirCommitsQuery, dirResponse)

	// Mock recent PRs (required for filtering)
	recentResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{
						map[string]any{
							"number": float64(50), "merged": true, "mergedAt": "2024-01-01T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{map[string]any{"author": map[string]any{"login": "dave"}}}},
						},
						map[string]any{
							"number": float64(49), "merged": true, "mergedAt": "2024-01-01T11:00:00Z",
							"author": map[string]any{"login": "charlie"}, "mergedBy": map[string]any{"login": "bob"},
							"reviews": map[string]any{"nodes": []any{map[string]any{"author": map[string]any{"login": "eve"}}}},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(recentPRsQuery, recentResponse)

	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1, "charlie": 2, "dave": 0, "eve": 1})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(candidates) == 0 {
		t.Fatal("expected candidates from directory commits")
	}

	// Verify directory contributors are in results
	found := make(map[string]bool)
	for _, c := range candidates {
		found[c.Username] = true
	}

	// bob, charlie, dave should be candidates from directory commits
	if !found["bob"] && !found["charlie"] && !found["dave"] {
		t.Error("expected at least one directory contributor in candidates")
	}
}

func TestFinder_Find_WithRecentActivity(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		ChangedFiles: []types.ChangedFile{
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetWriteAccess("test-owner", "test-repo", "charlie", true)
	client.SetWriteAccess("test-owner", "test-repo", "dave", true)

	// Mock recent PRs with multiple entries to build activity scores
	recentResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{
						// Bob is very active (appears in many PRs)
						map[string]any{
							"number": float64(100), "merged": true, "mergedAt": "2024-01-10T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{}},
						},
						map[string]any{
							"number": float64(99), "merged": true, "mergedAt": "2024-01-09T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{}},
						},
						map[string]any{
							"number": float64(98), "merged": true, "mergedAt": "2024-01-08T12:00:00Z",
							"author": map[string]any{"login": "charlie"}, "mergedBy": map[string]any{"login": "bob"},
							"reviews": map[string]any{"nodes": []any{map[string]any{"author": map[string]any{"login": "dave"}}}},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(recentPRsQuery, recentResponse)

	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1, "charlie": 2, "dave": 0})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Candidates should include users from recent activity
	if len(candidates) == 0 {
		t.Fatal("expected candidates from recent activity")
	}

	found := make(map[string]bool)
	for _, c := range candidates {
		found[c.Username] = true
	}

	// bob and charlie are in recent PRs
	if !found["bob"] && !found["charlie"] {
		t.Error("expected recent activity contributors in candidates")
	}
}

func TestFinder_Find_FiltersByRecentActivity(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"bob", "inactive-user"}, // inactive-user is assigned but not in recent PRs
		ChangedFiles: []types.ChangedFile{
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "inactive-user"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("inactive-user", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetWriteAccess("test-owner", "test-repo", "charlie", true)
	client.SetWriteAccess("test-owner", "test-repo", "inactive-user", true)

	// Recent PRs only includes bob and charlie (not inactive-user)
	recentResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{
						map[string]any{
							"number": float64(100), "merged": true, "mergedAt": "2024-01-10T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{}},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(recentPRsQuery, recentResponse)

	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1, "charlie": 2, "inactive-user": 0})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// inactive-user should be filtered out (not in recent 200 PRs)
	for _, c := range candidates {
		if c.Username == "inactive-user" {
			t.Error("inactive-user should be filtered out (not in recent activity)")
		}
	}

	// bob should be in candidates (is in recent PRs)
	found := false
	for _, c := range candidates {
		if c.Username == "bob" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'bob' in candidates (is in recent activity)")
	}
}

func TestFinder_Find_AssigneeIsPRAuthor(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"alice", "bob"}, // alice is both author and assignee
		ChangedFiles: []types.ChangedFile{
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team to trigger optimized path
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)

	// Recent PRs includes bob
	recentResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{
						map[string]any{
							"number": float64(100), "merged": true, "mergedAt": "2024-01-10T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{}},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(recentPRsQuery, recentResponse)
	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// alice should NOT be in candidates (is PR author even though assigned)
	for _, c := range candidates {
		if c.Username == "alice" {
			t.Error("PR author should not be in candidates even if assigned")
		}
	}
}

func TestFinder_Find_NoChangedFiles(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:        "test-owner",
		Repository:   "test-repo",
		Number:       1,
		Author:       "alice",
		ChangedFiles: []types.ChangedFile{}, // No changed files
	}

	// Configure large team
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetWriteAccess("test-owner", "test-repo", "charlie", true)

	// Recent PRs provides the only signal
	recentResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{
						map[string]any{
							"number": float64(100), "merged": true, "mergedAt": "2024-01-10T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{}},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(recentPRsQuery, recentResponse)
	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1, "charlie": 2})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still find candidates from recent activity alone
	if len(candidates) == 0 {
		t.Fatal("expected candidates from recent activity even without changed files")
	}
}

func TestFinder_Find_NoValidCandidates(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		ChangedFiles: []types.ChangedFile{
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team (>2 non-bot members) to trigger optimized path
	// All non-authors either lack write access or are not in recent PRs
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave", "eve"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)
	client.SetBotUser("eve", false)
	// None have write access
	client.SetWriteAccess("test-owner", "test-repo", "bob", false)
	client.SetWriteAccess("test-owner", "test-repo", "charlie", false)
	client.SetWriteAccess("test-owner", "test-repo", "dave", false)
	client.SetWriteAccess("test-owner", "test-repo", "eve", false)

	// Recent PRs with the users who lack write access
	recentResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{
						map[string]any{
							"number": float64(100), "merged": true, "mergedAt": "2024-01-10T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{}},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(recentPRsQuery, recentResponse)

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return empty list when no valid candidates (all filtered due to no write access)
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates when all are invalid, got %d", len(candidates))
	}
}

func TestFinder_Find_RecentPRsError(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"bob"},
		ChangedFiles: []types.ChangedFile{
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)

	// Set GraphQL error for recent PRs query (uses SetError with method:query key)
	client.SetError("MakeGraphQLRequest:"+recentPRsQuery, fmt.Errorf("network error"))

	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still return candidates even if recent PRs fails
	// (assignee should still be included, recent activity filter is skipped when empty)
	if len(candidates) == 0 {
		t.Error("expected candidates even when recent PRs fetch fails")
	}
}

func TestFinder_Find_DirectoryCommitsWithExistingCandidates(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"bob"}, // bob is assignee (existing candidate)
		ChangedFiles: []types.ChangedFile{
			{Filename: "pkg/main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave", "eve"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)
	client.SetBotUser("eve", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetWriteAccess("test-owner", "test-repo", "charlie", true)
	client.SetWriteAccess("test-owner", "test-repo", "dave", true)
	client.SetWriteAccess("test-owner", "test-repo", "eve", true)

	// Mock directory commits where bob appears as author, merger, and reviewer
	// This exercises the "existing candidate" branches for dir-author, dir-merger, dir-reviewer
	dirResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"name": "main",
					"target": map[string]any{
						"history": map[string]any{
							"nodes": []any{
								// PR 50: bob is author, charlie is merger, dave is reviewer
								map[string]any{
									"oid": "abc123",
									"author": map[string]any{
										"user": map[string]any{"login": "bob"},
									},
									"associatedPullRequests": map[string]any{
										"nodes": []any{
											map[string]any{
												"number":   float64(50),
												"merged":   true,
												"mergedAt": "2024-01-01T12:00:00Z",
												"author":   map[string]any{"login": "bob"},
												"mergedBy": map[string]any{"login": "charlie"},
												"reviews": map[string]any{
													"nodes": []any{
														map[string]any{"author": map[string]any{"login": "dave"}},
													},
												},
											},
										},
									},
								},
								// PR 51: bob is author again (exercises dir-author += branch)
								map[string]any{
									"oid": "def456",
									"author": map[string]any{
										"user": map[string]any{"login": "bob"},
									},
									"associatedPullRequests": map[string]any{
										"nodes": []any{
											map[string]any{
												"number":   float64(51),
												"merged":   true,
												"mergedAt": "2024-01-02T12:00:00Z",
												"author":   map[string]any{"login": "bob"},
												"mergedBy": map[string]any{"login": "bob"}, // bob merged own PR
												"reviews": map[string]any{
													"nodes": []any{
														map[string]any{"author": map[string]any{"login": "bob"}}, // bob reviewed
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(dirCommitsQuery, dirResponse)

	// Recent PRs includes all of them
	recentResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{
						map[string]any{
							"number": float64(50), "merged": true, "mergedAt": "2024-01-01T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{map[string]any{"author": map[string]any{"login": "dave"}}}},
						},
						map[string]any{
							"number": float64(51), "merged": true, "mergedAt": "2024-01-02T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "bob"},
							"reviews": map[string]any{"nodes": []any{}},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(recentPRsQuery, recentResponse)
	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1, "charlie": 2, "dave": 0})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// bob should be in candidates with scores from multiple sources
	found := false
	for _, c := range candidates {
		if c.Username == "bob" {
			found = true
			// Should have scores from assignee + dir-author + dir-merger + dir-reviewer
			if c.ContextScore <= 200 { // 200 is just the assignee score
				t.Errorf("expected bob to have bonus points from directory commits, got score %d", c.ContextScore)
			}
			break
		}
	}
	if !found {
		t.Error("expected 'bob' in candidates")
	}
}

func TestFinder_Find_DirectoryCommitsEmptyPRs(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"bob"},
		ChangedFiles: []types.ChangedFile{
			{Filename: "pkg/main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)

	// Mock directory commits with empty history (no PRs)
	dirResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"name": "main",
					"target": map[string]any{
						"history": map[string]any{
							"nodes": []any{}, // Empty - no commits in directory
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(dirCommitsQuery, dirResponse)

	// Recent PRs includes bob
	recentResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{
						map[string]any{
							"number": float64(100), "merged": true, "mergedAt": "2024-01-10T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{}},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(recentPRsQuery, recentResponse)
	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still return bob (from assignee) even with empty directory commits
	found := false
	for _, c := range candidates {
		if c.Username == "bob" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'bob' in candidates even with empty directory commits")
	}
}

func TestFinder_Find_DirectoryCommitsError(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"bob"},
		ChangedFiles: []types.ChangedFile{
			{Filename: "pkg/main.go", Additions: 10, Deletions: 5},
		},
	}

	// Configure large team
	client.SetCollaborators("test-owner", "test-repo", []string{"alice", "bob", "charlie", "dave"})
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)
	client.SetBotUser("dave", false)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)

	// Set GraphQL error for directory commits query (uses SetError with method:query key)
	client.SetError("MakeGraphQLRequest:"+dirCommitsQuery, fmt.Errorf("timeout"))

	// Recent PRs with bob
	recentResponse := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{
						map[string]any{
							"number": float64(100), "merged": true, "mergedAt": "2024-01-10T12:00:00Z",
							"author": map[string]any{"login": "bob"}, "mergedBy": map[string]any{"login": "charlie"},
							"reviews": map[string]any{"nodes": []any{}},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(recentPRsQuery, recentResponse)
	client.SetBatchOpenPRCount("test-owner", map[string]int{"bob": 1})

	candidates, err := finder.Find(ctx, pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still return candidates even if directory commits fails
	if len(candidates) == 0 {
		t.Error("expected candidates even when directory commits fetch fails")
	}
}
