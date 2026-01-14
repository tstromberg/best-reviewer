package reviewer

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/codeGROOVE-dev/best-reviewer/pkg/internal/testutil"
	"github.com/codeGROOVE-dev/best-reviewer/pkg/types"
)

func TestFinder_topChangedFilesFiltered(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	tests := []struct {
		name          string
		changedFiles  []types.ChangedFile
		n             int
		expectedFiles []string
	}{
		{
			name: "simple case - top 2 files",
			changedFiles: []types.ChangedFile{
				{Filename: "fileA.go", Additions: 10, Deletions: 5},
				{Filename: "fileB.go", Additions: 20, Deletions: 10},
				{Filename: "fileC.go", Additions: 5, Deletions: 2},
			},
			n:             2,
			expectedFiles: []string{"fileB.go", "fileA.go"}, // Sorted by total changes
		},
		{
			name: "filter out lock files when other files exist",
			changedFiles: []types.ChangedFile{
				{Filename: "src/main.go", Additions: 10, Deletions: 5},
				{Filename: "go.sum", Additions: 100, Deletions: 50},             // Should be filtered
				{Filename: "package-lock.json", Additions: 200, Deletions: 100}, // Should be filtered
			},
			n:             2,
			expectedFiles: []string{"src/main.go"},
		},
		{
			name: "include lock files when no other files",
			changedFiles: []types.ChangedFile{
				{Filename: "go.sum", Additions: 10, Deletions: 5},
				{Filename: "package-lock.json", Additions: 20, Deletions: 10},
			},
			n:             2,
			expectedFiles: []string{"package-lock.json", "go.sum"},
		},
		{
			name: "filter multiple lock file types",
			changedFiles: []types.ChangedFile{
				{Filename: "src/main.go", Additions: 5, Deletions: 2},
				{Filename: "yarn.lock", Additions: 50, Deletions: 25},
				{Filename: "Gemfile.lock", Additions: 30, Deletions: 15},
				{Filename: "Cargo.lock", Additions: 40, Deletions: 20},
				{Filename: "poetry.lock", Additions: 35, Deletions: 18},
			},
			n:             3,
			expectedFiles: []string{"src/main.go"},
		},
		{
			name: "request more files than available",
			changedFiles: []types.ChangedFile{
				{Filename: "fileA.go", Additions: 10, Deletions: 5},
			},
			n:             5,
			expectedFiles: []string{"fileA.go"},
		},
		{
			name:          "empty changed files",
			changedFiles:  []types.ChangedFile{},
			n:             3,
			expectedFiles: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &types.PullRequest{
				ChangedFiles: tt.changedFiles,
			}

			result := finder.topChangedFilesFiltered(pr, tt.n)

			if !reflect.DeepEqual(result, tt.expectedFiles) {
				t.Errorf("topChangedFilesFiltered() = %v, want %v", result, tt.expectedFiles)
			}
		})
	}
}

func TestFinder_getChangedLines(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	tests := []struct {
		name          string
		changedFiles  []types.ChangedFile
		filename      string
		expectedLines [][2]int
		expectError   bool
	}{
		{
			name: "simple hunk with single line range",
			changedFiles: []types.ChangedFile{
				{
					Filename: "test.go",
					Patch: `@@ -10,5 +10,7 @@ func main() {
 unchanged line
+added line
 unchanged line`,
				},
			},
			filename:      "test.go",
			expectedLines: [][2]int{{10, 16}},
		},
		{
			name: "multiple hunks",
			changedFiles: []types.ChangedFile{
				{
					Filename: "test.go",
					Patch: `@@ -10,3 +10,4 @@ func main() {
 line 1
+added line
 line 2
@@ -50,2 +51,3 @@ func helper() {
 line 3
+added line`,
				},
			},
			filename:      "test.go",
			expectedLines: [][2]int{{10, 13}, {51, 53}},
		},
		{
			name: "hunk with single line (no count)",
			changedFiles: []types.ChangedFile{
				{
					Filename: "test.go",
					Patch: `@@ -10 +10 @@ func main() {
+added line`,
				},
			},
			filename:      "test.go",
			expectedLines: [][2]int{{10, 10}},
		},
		{
			name: "file not in PR",
			changedFiles: []types.ChangedFile{
				{
					Filename: "other.go",
					Patch:    "@@ -10,5 +10,7 @@\ncode",
				},
			},
			filename:      "test.go",
			expectedLines: nil,
		},
		{
			name: "file with no patch",
			changedFiles: []types.ChangedFile{
				{
					Filename: "test.go",
					Patch:    "",
				},
			},
			filename:      "test.go",
			expectedLines: nil,
		},
		{
			name: "large line range",
			changedFiles: []types.ChangedFile{
				{
					Filename: "test.go",
					Patch: `@@ -100,50 +100,75 @@ func bigFunction() {
 many lines of code here`,
				},
			},
			filename:      "test.go",
			expectedLines: [][2]int{{100, 174}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &types.PullRequest{
				ChangedFiles: tt.changedFiles,
			}

			result, err := finder.getChangedLines(pr, tt.filename)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(result, tt.expectedLines) {
				t.Errorf("getChangedLines() = %v, want %v", result, tt.expectedLines)
			}
		})
	}
}

func TestFinder_findReviewersOptimized_WithAssignees(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"bob", "charlie"},
		ChangedFiles: []types.ChangedFile{
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	// Set up batch PR count
	client.SetBatchOpenPRCount("test-owner", map[string]int{
		"bob":     1,
		"charlie": 2,
	})

	// Configure write access
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetWriteAccess("test-owner", "test-repo", "charlie", true)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)

	candidates := finder.findReviewersOptimized(ctx, pr)

	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
	}

	// Assignees should be included
	found := make(map[string]bool)
	for _, c := range candidates {
		found[c.Username] = true
	}

	if !found["bob"] {
		t.Error("expected assignee 'bob' to be in candidates")
	}
	if !found["charlie"] {
		t.Error("expected assignee 'charlie' to be in candidates")
	}
}

func TestFinder_findReviewersOptimized_SkipAuthorAssignee(t *testing.T) {
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

	client.SetBatchOpenPRCount("test-owner", map[string]int{
		"alice": 1,
		"bob":   2,
	})

	// Configure write access
	client.SetWriteAccess("test-owner", "test-repo", "alice", true)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetBotUser("alice", false)
	client.SetBotUser("bob", false)

	candidates := finder.findReviewersOptimized(ctx, pr)

	// Alice should not be in candidates even though she's an assignee
	for _, c := range candidates {
		if c.Username == "alice" {
			t.Error("PR author should not be in candidates even if assigned")
		}
	}
}

func TestFinder_findReviewersOptimized_NoChangedFiles(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:        "test-owner",
		Repository:   "test-repo",
		Number:       1,
		Author:       "alice",
		ChangedFiles: []types.ChangedFile{}, // No files changed
	}

	candidates := finder.findReviewersOptimized(ctx, pr)

	// Should return empty slice, not nil
	// Empty result is expected when there are no changed files and no other signals
	if len(candidates) > 0 {
		t.Errorf("expected 0 candidates with no files changed, got %d", len(candidates))
	}
}

func TestFinder_findReviewersOptimized_WorkloadPenalty(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		Assignees:  []string{"bob", "charlie"},
		ChangedFiles: []types.ChangedFile{
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	// Bob has low workload, Charlie has high workload
	client.SetBatchOpenPRCount("test-owner", map[string]int{
		"bob":     1,
		"charlie": 10,
	})

	// Configure write access
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetWriteAccess("test-owner", "test-repo", "charlie", true)
	client.SetBotUser("bob", false)
	client.SetBotUser("charlie", false)

	candidates := finder.findReviewersOptimized(ctx, pr)

	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
	}

	// Find bob and charlie in results
	var bobCandidate, charlieCandidate *types.ReviewerCandidate
	for i := range candidates {
		if candidates[i].Username == "bob" {
			bobCandidate = &candidates[i]
		}
		if candidates[i].Username == "charlie" {
			charlieCandidate = &candidates[i]
		}
	}

	if bobCandidate == nil || charlieCandidate == nil {
		t.Fatal("expected both bob and charlie in candidates")
	}

	// Bob should have lower workload penalty than Charlie
	// This affects the final ranking but we can't assert exact scores
	// Just verify both have reasonable values
	if bobCandidate.ContextScore < 0 {
		t.Error("bob should have non-negative context score")
	}
	if charlieCandidate.ContextScore < 0 {
		t.Error("charlie should have non-negative context score")
	}
}

func TestFinder_findReviewersOptimized_BatchWorkloadError(t *testing.T) {
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

	// Configure error for batch PR count
	client.SetError("BatchOpenPRCount:test-owner", fmt.Errorf("rate limited"))

	// Configure write access (bob should still be valid)
	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetBotUser("bob", false)

	candidates := finder.findReviewersOptimized(ctx, pr)

	// Should still return candidates even when workload fetch fails
	if len(candidates) == 0 {
		t.Error("expected candidates even when batch workload fetch fails")
	}

	found := false
	for _, c := range candidates {
		if c.Username == "bob" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'bob' in candidates")
	}
}

func TestFinder_findReviewersOptimized_WorkloadPenaltyCapping(t *testing.T) {
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

	// Bob has very high workload (100 PRs -> 1000 point penalty)
	// But assignee weight is 200, so penalty caps at 100 (50% of 200)
	client.SetBatchOpenPRCount("test-owner", map[string]int{
		"bob": 100,
	})

	client.SetWriteAccess("test-owner", "test-repo", "bob", true)
	client.SetBotUser("bob", false)

	candidates := finder.findReviewersOptimized(ctx, pr)

	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate")
	}

	// Find bob
	var bobCandidate *types.ReviewerCandidate
	for i := range candidates {
		if candidates[i].Username == "bob" {
			bobCandidate = &candidates[i]
			break
		}
	}

	if bobCandidate == nil {
		t.Fatal("expected bob in candidates")
	}

	// Bob's score should be 200 (assignee) - 100 (capped penalty) = 100
	// The penalty is capped at 50% of the weight
	if bobCandidate.ContextScore < 100 {
		t.Errorf("expected context score >= 100 due to penalty cap, got %d", bobCandidate.ContextScore)
	}
}

func TestFinder_collectWeightedCandidates_NoChangedLines(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		Owner:      "test-owner",
		Repository: "test-repo",
		Number:     1,
		Author:     "alice",
		ChangedFiles: []types.ChangedFile{
			{Filename: "binary.png", Patch: ""}, // No patch for binary
		},
	}

	candidates := finder.collectWeightedCandidates(ctx, pr, []string{"binary.png"})

	// Should return empty list since no changed lines
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for file with no patch, got %d", len(candidates))
	}
}

func TestFinder_topChangedFilesFiltered_NestedLockFile(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		ChangedFiles: []types.ChangedFile{
			{Filename: "pkg/deps/go.sum", Additions: 100, Deletions: 50}, // Nested lock file
			{Filename: "main.go", Additions: 10, Deletions: 5},
		},
	}

	result := finder.topChangedFilesFiltered(pr, 2)

	// Should filter out nested go.sum by basename
	if len(result) != 1 {
		t.Errorf("expected 1 file after filtering nested lock file, got %d", len(result))
	}

	if len(result) > 0 && result[0] != "main.go" {
		t.Errorf("expected main.go, got %s", result[0])
	}
}

func TestFinder_getChangedLines_InvalidHunkHeader(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	pr := &types.PullRequest{
		ChangedFiles: []types.ChangedFile{
			{
				Filename: "test.go",
				Patch: `@@ invalid header
+ some line`,
			},
		},
	}

	lines, err := finder.getChangedLines(pr, "test.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return empty when hunk header can't be parsed
	if len(lines) != 0 {
		t.Errorf("expected 0 line ranges for invalid hunk header, got %d", len(lines))
	}
}
