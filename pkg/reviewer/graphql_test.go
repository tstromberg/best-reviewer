package reviewer

import (
	"context"
	"testing"
	"time"

	"github.com/codeGROOVE-dev/best-reviewer/pkg/internal/testutil"
)

func TestFinder_parseBlameResults_HappyPath(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Simulate a GraphQL blame response (matches actual structure from graphql.go)
	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{
							"ranges": []any{
								map[string]any{
									"startingLine": float64(10),
									"endingLine":   float64(15),
									"commit": map[string]any{
										"oid": "abc123",
										"associatedPullRequests": map[string]any{
											"nodes": []any{
												map[string]any{
													"number":   float64(42),
													"merged":   true,
													"mergedAt": time.Now().Add(-24 * time.Hour).Format(time.RFC3339),
													"author": map[string]any{
														"login": "alice",
													},
													"mergedBy": map[string]any{
														"login": "bob",
													},
													"reviews": map[string]any{
														"nodes": []any{
															map[string]any{
																"author": map[string]any{
																	"login": "charlie",
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
				},
			},
		},
	}

	lineRanges := [][2]int{{10, 20}}
	overlappingPRs, allPRs := finder.parseBlameResults(result, lineRanges)

	// Should find PRs that overlap with the line range
	if len(overlappingPRs) != 1 {
		t.Fatalf("expected 1 overlapping PR, got %d", len(overlappingPRs))
	}

	// allPRs contains only non-overlapping PRs, so should be empty
	if len(allPRs) != 0 {
		t.Errorf("expected 0 in allPRs (overlapping PRs don't go there), got %d", len(allPRs))
	}

	if overlappingPRs[0].Number != 42 {
		t.Errorf("expected PR number 42, got %d", overlappingPRs[0].Number)
	}

	if overlappingPRs[0].Author != "alice" {
		t.Errorf("expected author alice, got %s", overlappingPRs[0].Author)
	}
}

func TestFinder_parseBlameResults_NoOverlap(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{
							"ranges": []any{
								map[string]any{
									"startingLine": float64(100),
									"endingLine":   float64(110),
									"commit": map[string]any{
										"oid": "abc123",
										"associatedPullRequests": map[string]any{
											"nodes": []any{
												map[string]any{
													"number":   float64(42),
													"merged":   true,
													"mergedAt": time.Now().Add(-24 * time.Hour).Format(time.RFC3339),
													"author": map[string]any{
														"login": "alice",
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

	lineRanges := [][2]int{{10, 20}} // No overlap with 100-110
	overlappingPRs, allPRs := finder.parseBlameResults(result, lineRanges)

	if len(overlappingPRs) != 0 {
		t.Errorf("expected 0 overlapping PRs, got %d", len(overlappingPRs))
	}

	if len(allPRs) != 1 {
		t.Errorf("expected 1 total PR (even without overlap), got %d", len(allPRs))
	}
}

func TestFinder_parseBlameResults_NoDataField(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Empty result - no data field
	result := map[string]any{}
	overlapping, all := finder.parseBlameResults(result, [][2]int{{10, 15}})

	if len(overlapping) != 0 {
		t.Errorf("expected 0 overlapping PRs, got %d", len(overlapping))
	}
	if len(all) != 0 {
		t.Errorf("expected 0 all PRs, got %d", len(all))
	}
}

func TestFinder_parseBlameResults_NoRepositoryField(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Missing repository field
	result := map[string]any{
		"data": map[string]any{},
	}
	overlapping, all := finder.parseBlameResults(result, [][2]int{{10, 15}})

	if len(overlapping) != 0 || len(all) != 0 {
		t.Error("expected empty results for missing repository field")
	}
}

func TestFinder_parseBlameResults_NoBlameField(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Missing blame field
	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{},
				},
			},
		},
	}
	overlapping, all := finder.parseBlameResults(result, [][2]int{{10, 15}})

	if len(overlapping) != 0 || len(all) != 0 {
		t.Error("expected empty results for missing blame field")
	}
}

func TestParsePRNode_NoReviews(t *testing.T) {
	pr := map[string]any{
		"number": float64(1),
		"merged": true,
	}
	prInfo := parsePRNode(pr)

	if prInfo == nil {
		t.Fatal("expected non-nil PR")
	}
	if len(prInfo.Reviewers) != 0 {
		t.Errorf("expected 0 reviewers, got %d", len(prInfo.Reviewers))
	}
}

func TestParsePRNode_ReviewsNoNodes(t *testing.T) {
	pr := map[string]any{
		"number":  float64(1),
		"merged":  true,
		"reviews": map[string]any{},
	}
	prInfo := parsePRNode(pr)

	if prInfo == nil {
		t.Fatal("expected non-nil PR")
	}
	if len(prInfo.Reviewers) != 0 {
		t.Errorf("expected 0 reviewers, got %d", len(prInfo.Reviewers))
	}
}

func TestParsePRNode_InvalidReviewNode(t *testing.T) {
	pr := map[string]any{
		"number": float64(1),
		"merged": true,
		"reviews": map[string]any{
			"nodes": []any{
				"not a map",
			},
		},
	}
	prInfo := parsePRNode(pr)

	if prInfo == nil {
		t.Fatal("expected non-nil PR")
	}
	if len(prInfo.Reviewers) != 0 {
		t.Errorf("expected 0 reviewers, got %d", len(prInfo.Reviewers))
	}
}

func TestParsePRNode_ReviewNoAuthor(t *testing.T) {
	pr := map[string]any{
		"number": float64(1),
		"merged": true,
		"reviews": map[string]any{
			"nodes": []any{
				map[string]any{},
			},
		},
	}
	prInfo := parsePRNode(pr)

	if prInfo == nil {
		t.Fatal("expected non-nil PR")
	}
	if len(prInfo.Reviewers) != 0 {
		t.Errorf("expected 0 reviewers, got %d", len(prInfo.Reviewers))
	}
}

func TestParsePRNode_DuplicateReviewers(t *testing.T) {
	pr := map[string]any{
		"number": float64(1),
		"merged": true,
		"reviews": map[string]any{
			"nodes": []any{
				map[string]any{
					"author": map[string]any{
						"login": "alice",
					},
				},
				map[string]any{
					"author": map[string]any{
						"login": "alice",
					},
				},
			},
		},
	}
	prInfo := parsePRNode(pr)

	if prInfo == nil {
		t.Fatal("expected non-nil PR")
	}
	if len(prInfo.Reviewers) != 1 {
		t.Errorf("expected 1 reviewer (deduplicated), got %d", len(prInfo.Reviewers))
	}
	if prInfo.Reviewers[0] != "alice" {
		t.Errorf("expected reviewer 'alice', got %q", prInfo.Reviewers[0])
	}
}

func TestFinder_parseBlameResults_NoRanges(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Missing ranges field
	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{},
					},
				},
			},
		},
	}
	overlapping, all := finder.parseBlameResults(result, [][2]int{{10, 15}})

	if len(overlapping) != 0 || len(all) != 0 {
		t.Error("expected empty results for missing ranges field")
	}
}

func TestFinder_parseBlameResults_InvalidRange(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Invalid range (missing startingLine)
	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{
							"ranges": []any{
								map[string]any{
									"endingLine": float64(15),
									// Missing startingLine
									"commit": map[string]any{},
								},
							},
						},
					},
				},
			},
		},
	}
	overlapping, all := finder.parseBlameResults(result, [][2]int{{10, 15}})

	if len(overlapping) != 0 || len(all) != 0 {
		t.Error("expected empty results for invalid range")
	}
}

func TestFinder_parseBlameResults_InvalidPRNode(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Invalid prNode (not a map)
	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{
							"ranges": []any{
								map[string]any{
									"startingLine": float64(10),
									"endingLine":   float64(15),
									"commit": map[string]any{
										"associatedPullRequests": map[string]any{
											"nodes": []any{
												"invalid-node", // Not a map
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
	overlapping, all := finder.parseBlameResults(result, [][2]int{{10, 15}})

	if len(overlapping) != 0 || len(all) != 0 {
		t.Error("expected empty results for invalid PR node")
	}
}

func TestFinder_parseBlameResults_NotMergedPR(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// PR that is not merged
	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{
							"ranges": []any{
								map[string]any{
									"startingLine": float64(10),
									"endingLine":   float64(15),
									"commit": map[string]any{
										"associatedPullRequests": map[string]any{
											"nodes": []any{
												map[string]any{
													"number": float64(123),
													"merged": false, // Not merged
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
	overlapping, all := finder.parseBlameResults(result, [][2]int{{10, 15}})

	if len(overlapping) != 0 || len(all) != 0 {
		t.Error("expected empty results for non-merged PR")
	}
}

func TestFinder_parseBlameResults_DirectCommitWithNoAuthor(t *testing.T) {
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Direct commit with no PR and no author
	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{
							"ranges": []any{
								map[string]any{
									"startingLine": float64(10),
									"endingLine":   float64(15),
									"commit": map[string]any{
										"associatedPullRequests": map[string]any{
											"nodes": []any{}, // No PRs
										},
										// No author field
									},
								},
							},
						},
					},
				},
			},
		},
	}
	overlapping, all := finder.parseBlameResults(result, [][2]int{{10, 15}})

	if len(overlapping) != 0 || len(all) != 0 {
		t.Error("expected empty results for commit with no PR and no author")
	}
}

func TestFinder_recentCommitsInDirectory_CacheTypeAssertionFails(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Set invalid cached value (wrong type)
	cacheKey := "commits-dir:owner/repo:src:10"
	_ = client.Cache().SetAsyncTTL(ctx, cacheKey, "invalid-type", time.Hour) //nolint:errcheck // Testing cache type assertion failure

	// Mock a successful GraphQL response
	response := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"history": map[string]any{
							"nodes": []any{
								map[string]any{
									"oid": "abc123",
									"author": map[string]any{
										"user": map[string]any{
											"login": "alice",
										},
									},
									"associatedPullRequests": map[string]any{
										"nodes": []any{},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	query := `
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

	client.SetGraphQLResponse(query, response)

	// Should fall through cache type assertion and make GraphQL request
	prs, err := finder.recentCommitsInDirectory(ctx, "owner", "repo", "src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get results from GraphQL query
	if len(prs) != 1 {
		t.Errorf("expected 1 PR from GraphQL query, got %d", len(prs))
	}
}

func TestFinder_recentPRsInProject_CacheTypeAssertionFails(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Set invalid cached value (wrong type)
	cacheKey := "prs-project:owner/repo"
	_ = client.Cache().SetAsyncTTL(ctx, cacheKey, 12345, time.Hour) //nolint:errcheck // Testing cache type assertion failure

	// Mock a successful GraphQL response
	response := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{
						"hasNextPage": false,
					},
					"nodes": []any{
						map[string]any{
							"number":   float64(100),
							"merged":   true,
							"mergedAt": "2024-01-01T12:00:00Z",
							"author": map[string]any{
								"login": "bob",
							},
							"mergedBy": map[string]any{
								"login": "alice",
							},
							"reviews": map[string]any{
								"nodes": []any{},
							},
						},
					},
				},
			},
		},
	}

	query := `
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

	client.SetGraphQLResponse(query, response)

	// Should fall through cache type assertion and make GraphQL request
	prs, err := finder.recentPRsInProject(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get results from GraphQL query
	if len(prs) != 1 {
		t.Errorf("expected 1 PR from GraphQL query, got %d", len(prs))
	}

	if prs[0].Number != 100 {
		t.Errorf("expected PR #100, got #%d", prs[0].Number)
	}
}

func TestFinder_parseDirectoryCommitsFromGraphQL_NoNodes(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"history": map[string]any{
							// Missing "nodes" field
						},
					},
				},
			},
		},
	}
	prs := finder.parseDirectoryCommitsFromGraphQL(result)

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs, got %d", len(prs))
	}
}

func TestFinder_blameForLines_GraphQLError(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	client.SetGraphQLError("GraphQL request failed")

	_, _, err := finder.blameForLines(ctx, "owner", "repo", "file.go", [][2]int{{1, 10}})
	if err == nil {
		t.Error("expected error from GraphQL failure")
	}
}

func TestFinder_parseBlameResults_MissingDefaultBranchRef(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				// Missing defaultBranchRef
			},
		},
	}
	overlapping, all := finder.parseBlameResults(result, [][2]int{{1, 10}})

	if len(overlapping) != 0 || len(all) != 0 {
		t.Error("expected empty results when defaultBranchRef is missing")
	}
}

func TestFinder_parseBlameResults_MissingTarget(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					// Missing target
				},
			},
		},
	}
	overlapping, all := finder.parseBlameResults(result, [][2]int{{1, 10}})

	if len(overlapping) != 0 || len(all) != 0 {
		t.Error("expected empty results when target is missing")
	}
}

func TestFinder_parseBlameResults_DirectCommitWithAuthor(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	// Commit with no associated PRs but has an author (direct push to main)
	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{
							"ranges": []any{
								map[string]any{
									"startingLine": float64(10),
									"endingLine":   float64(15),
									"commit": map[string]any{
										"oid": "abc123",
										"author": map[string]any{
											"user": map[string]any{
												"login": "direct-committer",
											},
										},
										"associatedPullRequests": map[string]any{
											"nodes": []any{}, // Empty - no PR
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
	overlapping, all := finder.parseBlameResults(result, [][2]int{{10, 15}})

	// Direct commits should be captured as overlapping since they touch the lines
	if len(overlapping) != 1 {
		t.Errorf("expected 1 overlapping entry for direct commit, got %d", len(overlapping))
	}
	if len(overlapping) > 0 && overlapping[0].Author != "direct-committer" {
		t.Errorf("expected author 'direct-committer', got %q", overlapping[0].Author)
	}
	if len(all) != 0 {
		t.Errorf("expected 0 in all (direct commit is overlapping), got %d", len(all))
	}
}

func TestFinder_parseBlameResults_DirectCommitNonOverlapping(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	// Commit with no associated PRs but has an author, lines don't overlap
	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{
							"ranges": []any{
								map[string]any{
									"startingLine": float64(100),
									"endingLine":   float64(110),
									"commit": map[string]any{
										"oid": "abc123",
										"author": map[string]any{
											"user": map[string]any{
												"login": "another-committer",
											},
										},
										"associatedPullRequests": map[string]any{
											"nodes": []any{}, // Empty - no PR
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
	overlapping, all := finder.parseBlameResults(result, [][2]int{{1, 10}})

	// Non-overlapping direct commits should go to all
	if len(overlapping) != 0 {
		t.Errorf("expected 0 overlapping (lines don't match), got %d", len(overlapping))
	}
	if len(all) != 1 {
		t.Errorf("expected 1 non-overlapping direct commit in all, got %d", len(all))
	}
}

func TestFinder_recentCommitsInDirectory_GraphQLError(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	client.SetGraphQLError("GraphQL request failed")

	_, err := finder.recentCommitsInDirectory(ctx, "owner", "repo", "src")
	if err == nil {
		t.Error("expected error from GraphQL failure")
	}
}

func TestFinder_recentPRsInProject_GraphQLError(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	client.SetGraphQLError("GraphQL request failed")

	_, err := finder.recentPRsInProject(ctx, "owner", "repo")
	if err == nil {
		t.Error("expected error from GraphQL failure")
	}
}

func TestFinder_parseProjectPRsFromGraphQL_MissingData(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		// Missing data field
	}
	prs, err := finder.parseProjectPRsFromGraphQL(result)
	if err == nil {
		t.Error("expected error when data missing")
	}

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs when data missing, got %d", len(prs))
	}
}

func TestFinder_parseProjectPRsFromGraphQL_MissingRepository(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			// Missing repository
		},
	}
	prs, err := finder.parseProjectPRsFromGraphQL(result)
	if err == nil {
		t.Error("expected error when repository missing")
	}

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs when repository missing, got %d", len(prs))
	}
}

func TestFinder_parseProjectPRsFromGraphQL_MissingPullRequests(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				// Missing pullRequests
			},
		},
	}
	prs, err := finder.parseProjectPRsFromGraphQL(result)
	if err == nil {
		t.Error("expected error when pullRequests missing")
	}

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs when pullRequests missing, got %d", len(prs))
	}
}

func TestFinder_parseDirectoryCommitsFromGraphQL_MissingRepository(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			// Missing repository
		},
	}
	prs := finder.parseDirectoryCommitsFromGraphQL(result)

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs when repository missing, got %d", len(prs))
	}
}

func TestFinder_parseDirectoryCommitsFromGraphQL_MissingTarget(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					// Missing target
				},
			},
		},
	}
	prs := finder.parseDirectoryCommitsFromGraphQL(result)

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs when target missing, got %d", len(prs))
	}
}

func TestFinder_parseDirectoryCommitsFromGraphQL_MissingHistory(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						// Missing history
					},
				},
			},
		},
	}
	prs := finder.parseDirectoryCommitsFromGraphQL(result)

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs when history missing, got %d", len(prs))
	}
}

func TestFinder_parseProjectPRsFromGraphQL_MissingNodes(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					// Missing nodes field
				},
			},
		},
	}
	_, err := finder.parseProjectPRsFromGraphQL(result)

	if err == nil {
		t.Error("expected error when nodes missing")
	}
}

func TestFinder_parseProjectPRsFromGraphQL_InvalidPRNode(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"nodes": []any{
						"invalid-node", // Not a map
					},
				},
			},
		},
	}
	prs, err := finder.parseProjectPRsFromGraphQL(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs for invalid node, got %d", len(prs))
	}
}

func TestFinder_parsePRNode_NotMerged(t *testing.T) {
	pr := map[string]any{
		"number": float64(100),
		"merged": false, // Not merged
	}

	result := parsePRNode(pr)

	if result != nil {
		t.Error("expected nil for unmerged PR")
	}
}

func TestFinder_parsePRNode_MissingNumber(t *testing.T) {
	pr := map[string]any{
		"merged": true,
		// Missing number field
	}

	result := parsePRNode(pr)

	if result != nil {
		t.Error("expected nil for missing PR number")
	}
}

func TestFinder_parsePRNode_InvalidNumberType(t *testing.T) {
	pr := map[string]any{
		"number": "not-a-number", // Wrong type
		"merged": true,
	}

	result := parsePRNode(pr)

	if result != nil {
		t.Error("expected nil for invalid number type")
	}
}

func TestFinder_parsePRNode_MissingAuthor(t *testing.T) {
	pr := map[string]any{
		"number":   float64(100),
		"merged":   true,
		"mergedAt": "2024-01-01T12:00:00Z",
		// Missing author
	}

	result := parsePRNode(pr)

	if result == nil {
		t.Fatal("expected non-nil PR info")
	}
	if result.Author != "" {
		t.Errorf("expected empty author, got %q", result.Author)
	}
}

func TestFinder_parsePRNode_MissingMergedBy(t *testing.T) {
	pr := map[string]any{
		"number":   float64(100),
		"merged":   true,
		"mergedAt": "2024-01-01T12:00:00Z",
		"author": map[string]any{
			"login": "alice",
		},
		// Missing mergedBy
	}

	result := parsePRNode(pr)

	if result == nil {
		t.Fatal("expected non-nil PR info")
	}
	if result.MergedBy != "" {
		t.Errorf("expected empty mergedBy, got %q", result.MergedBy)
	}
}

func TestFinder_blameForLines_EmptyLineRanges(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	overlapping, filePRs, err := finder.blameForLines(ctx, "owner", "repo", "file.go", [][2]int{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(overlapping) != 0 || len(filePRs) != 0 {
		t.Error("expected empty results for empty line ranges")
	}
}

func TestFinder_blameForLines_GraphQLErrorsInResponse(t *testing.T) {
	ctx := context.Background()
	client := testutil.NewMockGitHubClient()
	finder := New(client, Config{PRCountCache: time.Hour})

	// Set up a response with GraphQL errors field (but not a transport error)
	query := `
	query($owner: String!, $repo: String!, $path: String!) {
		repository(owner: $owner, name: $repo) {
			defaultBranchRef {
				target {
					... on Commit {
						blame(path: $path) {
							ranges {
								startingLine
								endingLine
								commit {
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
		}
	}`

	response := map[string]any{
		"errors": []any{
			map[string]any{"message": "some GraphQL error"},
		},
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"blame": map[string]any{
							"ranges": []any{},
						},
					},
				},
			},
		},
	}
	client.SetGraphQLResponse(query, response)

	// Should not error - just log warning and return empty
	_, _, err := finder.blameForLines(ctx, "owner", "repo", "file.go", [][2]int{{1, 10}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFinder_parseDirectoryCommitsFromGraphQL_InvalidNodeType(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"history": map[string]any{
							"nodes": []any{
								"invalid-string-node", // Not a map
							},
						},
					},
				},
			},
		},
	}
	prs := finder.parseDirectoryCommitsFromGraphQL(result)

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs for invalid node type, got %d", len(prs))
	}
}

func TestFinder_parseDirectoryCommitsFromGraphQL_PRWithReviewer(t *testing.T) {
	finder := New(testutil.NewMockGitHubClient(), Config{PRCountCache: time.Hour})

	result := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"defaultBranchRef": map[string]any{
					"target": map[string]any{
						"history": map[string]any{
							"nodes": []any{
								map[string]any{
									"oid":             "abc123",
									"messageHeadline": "Fix bug",
									"author": map[string]any{
										"user": map[string]any{
											"login": "alice",
										},
									},
									"associatedPullRequests": map[string]any{
										"nodes": []any{
											map[string]any{
												"number":   float64(42),
												"merged":   true,
												"mergedAt": time.Now().Format(time.RFC3339),
												"author": map[string]any{
													"login": "alice",
												},
												"mergedBy": map[string]any{
													"login": "bob",
												},
												"reviews": map[string]any{
													"nodes": []any{
														map[string]any{
															"author": map[string]any{
																"login": "charlie",
															},
														},
														map[string]any{
															"author": map[string]any{
																"login": "dave",
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
			},
		},
	}
	prs := finder.parseDirectoryCommitsFromGraphQL(result)

	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}

	if len(prs[0].Reviewers) != 2 {
		t.Errorf("expected 2 reviewers, got %d", len(prs[0].Reviewers))
	}
}
