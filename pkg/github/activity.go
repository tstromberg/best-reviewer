package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/codeGROOVE-dev/best-reviewer/pkg/activity"
)

const activityLookbackDays = 60

// activityTimelineCacheTTL is how long to cache activity timelines (15 days).
const activityTimelineCacheTTL = 15 * 24 * time.Hour

// orgActivityQuery fetches user PR activity within an organization over the past 60 days.
// Uses search API to find PRs the user is involved with, then extracts timestamps from
// PR creation, merges, reviews, comments, commits, lifecycle events, and thread resolutions.
const orgActivityQuery = `query($search: String!, $after: String) {
  search(query: $search, type: ISSUE, first: 50, after: $after) {
    pageInfo {
      endCursor
      hasNextPage
    }
    nodes {
      ... on PullRequest {
        createdAt
        mergedAt
        author { login }
        mergedBy { login }
        reviews(first: 50) {
          nodes {
            submittedAt
            author { login }
            comments(first: 20) {
              nodes {
                createdAt
                author { login }
              }
            }
          }
        }
        comments(first: 30) {
          nodes {
            createdAt
            author { login }
          }
        }
        commits(first: 30) {
          nodes {
            commit {
              committedDate
              author {
                user { login }
              }
            }
          }
        }
        reviewThreads(first: 30) {
          nodes {
            isResolved
            resolvedBy { login }
            comments(first: 1) {
              nodes { createdAt }
            }
          }
        }
        timelineItems(first: 50, itemTypes: [CLOSED_EVENT, REOPENED_EVENT, CONVERT_TO_DRAFT_EVENT, READY_FOR_REVIEW_EVENT, REVIEW_DISMISSED_EVENT]) {
          nodes {
            ... on ClosedEvent {
              createdAt
              actor { login }
            }
            ... on ReopenedEvent {
              createdAt
              actor { login }
            }
            ... on ConvertToDraftEvent {
              createdAt
              actor { login }
            }
            ... on ReadyForReviewEvent {
              createdAt
              actor { login }
            }
            ... on ReviewDismissedEvent {
              createdAt
              actor { login }
            }
          }
        }
      }
    }
  }
}`

// FetchTimeline fetches a user's activity timeline within an organization over the past 60 days.
// Uses org-specific search to capture PR activity: creations, reviews, comments, and commits.
// The timeline buckets activity into 3-hour UTC windows for timing boost calculation.
// Implements the activity.Fetcher interface.
func (c *Client) FetchTimeline(ctx context.Context, org, username string) (*activity.Timeline, error) {
	// Check persistent cache first
	cacheKey := makeCacheKey("activity-timeline", org, username)
	if cached, found, err := c.cache.Get(ctx, cacheKey); err != nil {
		slog.Debug("Activity timeline cache read error", "key", cacheKey, "error", err)
	} else if found {
		if timeline, ok := cached.(*activity.Timeline); ok {
			slog.Info("Using cached activity timeline", "org", org, "user", username, "events", timeline.TotalEvents)
			return timeline, nil
		}
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -activityLookbackDays)

	// Build search query for PRs the user is involved with in the org
	searchQuery := fmt.Sprintf("org:%s involves:%s is:pr created:>%s", org, username, from.Format("2006-01-02"))

	timeline := &activity.Timeline{
		Username:  username,
		FetchedAt: now,
	}

	var cursor *string
	totalPages := 0
	maxPages := 4 // Limit to 200 PRs (4 pages * 50 results)

	for totalPages < maxPages {
		variables := map[string]any{
			"search": searchQuery,
			"after":  cursor,
		}

		result, err := c.MakeGraphQLRequest(ctx, orgActivityQuery, variables)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch org activity timeline: %w", err)
		}

		pageTimeline, nextCursor, hasMore := c.parseOrgActivityPage(result, username)

		// Merge page results into timeline
		for i := range activity.BucketCount {
			timeline.BucketCounts[i] += pageTimeline.BucketCounts[i]
		}
		timeline.TotalEvents += pageTimeline.TotalEvents

		totalPages++
		if !hasMore || nextCursor == nil {
			break
		}
		cursor = nextCursor
	}

	slog.Info("Fetched org activity timeline",
		"org", org,
		"user", username,
		"total_events", timeline.TotalEvents,
		"buckets", timeline.BucketCounts,
		"pages", totalPages)

	// Cache the result persistently
	if err := c.cache.SetAsyncTTL(ctx, cacheKey, timeline, activityTimelineCacheTTL); err != nil {
		slog.Warn("Failed to cache activity timeline", "key", cacheKey, "error", err)
	}

	return timeline, nil
}

// parseOrgActivityPage parses a page of org activity search results.
// Returns the timeline for this page, the next cursor, and whether there are more pages.
func (c *Client) parseOrgActivityPage(result map[string]any, username string) (*activity.Timeline, *string, bool) {
	timeline := &activity.Timeline{}

	data, ok := result["data"].(map[string]any)
	if !ok {
		return timeline, nil, false
	}

	search, ok := data["search"].(map[string]any)
	if !ok {
		return timeline, nil, false
	}

	// Get pagination info
	var nextCursor *string
	hasMore := false
	if pageInfo, ok := search["pageInfo"].(map[string]any); ok {
		if hasNext, ok := pageInfo["hasNextPage"].(bool); ok {
			hasMore = hasNext
		}
		if cursor, ok := pageInfo["endCursor"].(string); ok && hasMore {
			nextCursor = &cursor
		}
	}

	nodes, ok := search["nodes"].([]any)
	if !ok {
		return timeline, nextCursor, hasMore
	}

	for _, node := range nodes {
		prNode, ok := node.(map[string]any)
		if !ok {
			continue
		}

		// PR creation timestamp (only if user is author)
		if author, ok := prNode["author"].(map[string]any); ok {
			if login, ok := author["login"].(string); ok && login == username {
				if createdAt, ok := prNode["createdAt"].(string); ok {
					if ts, err := time.Parse(time.RFC3339, createdAt); err == nil {
						timeline.AddEvent(ts)
					}
				}
			}
		}

		// PR merge timestamp (only if user is the merger)
		c.parseMergeTimestamp(prNode, username, timeline)

		// Review timestamps and inline review comments (only for this user)
		c.parseReviewTimestamps(prNode, username, timeline)

		// Comment timestamps (only for this user's comments)
		c.parseCommentTimestamps(prNode, username, timeline)

		// Commit timestamps (only for this user's commits)
		c.parseCommitTimestamps(prNode, username, timeline)

		// Review thread resolutions (only for this user)
		c.parseThreadResolutions(prNode, username, timeline)

		// Timeline events: close, reopen, draft, ready for review, review dismissal
		c.parseTimelineEvents(prNode, username, timeline)
	}

	return timeline, nextCursor, hasMore
}

// parseMergeTimestamp extracts merge timestamp if the specified user merged the PR.
func (*Client) parseMergeTimestamp(prNode map[string]any, username string, timeline *activity.Timeline) {
	mergedBy, ok := prNode["mergedBy"].(map[string]any)
	if !ok {
		return
	}

	login, ok := mergedBy["login"].(string)
	if !ok || login != username {
		return
	}

	mergedAt, ok := prNode["mergedAt"].(string)
	if !ok {
		return
	}

	ts, err := time.Parse(time.RFC3339, mergedAt)
	if err != nil {
		return
	}
	timeline.AddEvent(ts)
}

// parseReviewTimestamps extracts review submission timestamps and inline review comments.
func (*Client) parseReviewTimestamps(prNode map[string]any, username string, timeline *activity.Timeline) {
	reviews, ok := prNode["reviews"].(map[string]any)
	if !ok {
		return
	}

	nodes, ok := reviews["nodes"].([]any)
	if !ok {
		return
	}

	for _, node := range nodes {
		review, ok := node.(map[string]any)
		if !ok {
			continue
		}

		// Check if this review is by the target user
		reviewAuthor, ok := review["author"].(map[string]any)
		if ok {
			if login, ok := reviewAuthor["login"].(string); ok && login == username {
				// Extract submittedAt timestamp for the review itself
				if submittedAt, ok := review["submittedAt"].(string); ok {
					if ts, err := time.Parse(time.RFC3339, submittedAt); err == nil {
						timeline.AddEvent(ts)
					}
				}
			}
		}

		// Extract inline review comments by this user (regardless of review author)
		comments, ok := review["comments"].(map[string]any)
		if !ok {
			continue
		}
		commentNodes, ok := comments["nodes"].([]any)
		if !ok {
			continue
		}
		for _, commentNode := range commentNodes {
			comment, ok := commentNode.(map[string]any)
			if !ok {
				continue
			}
			commentAuthor, ok := comment["author"].(map[string]any)
			if !ok {
				continue
			}
			login, ok := commentAuthor["login"].(string)
			if !ok || login != username {
				continue
			}
			createdAt, ok := comment["createdAt"].(string)
			if !ok {
				continue
			}
			if ts, err := time.Parse(time.RFC3339, createdAt); err == nil {
				timeline.AddEvent(ts)
			}
		}
	}
}

// parseCommentTimestamps extracts comment creation timestamps for the specified user.
func (*Client) parseCommentTimestamps(prNode map[string]any, username string, timeline *activity.Timeline) {
	comments, ok := prNode["comments"].(map[string]any)
	if !ok {
		return
	}

	nodes, ok := comments["nodes"].([]any)
	if !ok {
		return
	}

	for _, node := range nodes {
		comment, ok := node.(map[string]any)
		if !ok {
			continue
		}

		// Check if this comment is by the target user
		author, ok := comment["author"].(map[string]any)
		if !ok {
			continue
		}
		login, ok := author["login"].(string)
		if !ok || login != username {
			continue
		}

		// Extract createdAt timestamp
		createdAt, ok := comment["createdAt"].(string)
		if !ok {
			continue
		}
		ts, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			continue
		}
		timeline.AddEvent(ts)
	}
}

// parseCommitTimestamps extracts commit timestamps for the specified user.
func (*Client) parseCommitTimestamps(prNode map[string]any, username string, timeline *activity.Timeline) {
	commits, ok := prNode["commits"].(map[string]any)
	if !ok {
		return
	}

	nodes, ok := commits["nodes"].([]any)
	if !ok {
		return
	}

	for _, node := range nodes {
		commitNode, ok := node.(map[string]any)
		if !ok {
			continue
		}

		commit, ok := commitNode["commit"].(map[string]any)
		if !ok {
			continue
		}

		// Check if this commit is by the target user
		author, ok := commit["author"].(map[string]any)
		if !ok {
			continue
		}
		user, ok := author["user"].(map[string]any)
		if !ok {
			continue
		}
		login, ok := user["login"].(string)
		if !ok || login != username {
			continue
		}

		// Extract committedDate timestamp
		committedDate, ok := commit["committedDate"].(string)
		if !ok {
			continue
		}
		ts, err := time.Parse(time.RFC3339, committedDate)
		if err != nil {
			continue
		}
		timeline.AddEvent(ts)
	}
}

// parseThreadResolutions extracts review thread resolution timestamps for the specified user.
func (*Client) parseThreadResolutions(prNode map[string]any, username string, timeline *activity.Timeline) {
	reviewThreads, ok := prNode["reviewThreads"].(map[string]any)
	if !ok {
		return
	}

	nodes, ok := reviewThreads["nodes"].([]any)
	if !ok {
		return
	}

	for _, node := range nodes {
		thread, ok := node.(map[string]any)
		if !ok {
			continue
		}

		// Check if thread is resolved
		isResolved, ok := thread["isResolved"].(bool)
		if !ok || !isResolved {
			continue
		}

		// Check if resolved by the target user
		resolvedBy, ok := thread["resolvedBy"].(map[string]any)
		if !ok {
			continue
		}
		login, ok := resolvedBy["login"].(string)
		if !ok || login != username {
			continue
		}

		// Use the first comment's createdAt as approximation for resolution time
		// (GitHub doesn't expose resolvedAt directly in GraphQL)
		comments, ok := thread["comments"].(map[string]any)
		if !ok {
			continue
		}
		commentNodes, ok := comments["nodes"].([]any)
		if !ok || len(commentNodes) == 0 {
			continue
		}
		firstComment, ok := commentNodes[0].(map[string]any)
		if !ok {
			continue
		}
		createdAt, ok := firstComment["createdAt"].(string)
		if !ok {
			continue
		}
		ts, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			continue
		}
		timeline.AddEvent(ts)
	}
}

// parseTimelineEvents extracts PR lifecycle events (close, reopen, draft, ready, dismissal).
func (*Client) parseTimelineEvents(prNode map[string]any, username string, timeline *activity.Timeline) {
	timelineItems, ok := prNode["timelineItems"].(map[string]any)
	if !ok {
		return
	}

	nodes, ok := timelineItems["nodes"].([]any)
	if !ok {
		return
	}

	for _, node := range nodes {
		event, ok := node.(map[string]any)
		if !ok {
			continue
		}

		// Check if actor is the target user
		actor, ok := event["actor"].(map[string]any)
		if !ok {
			continue
		}
		login, ok := actor["login"].(string)
		if !ok || login != username {
			continue
		}

		// Extract createdAt timestamp
		createdAt, ok := event["createdAt"].(string)
		if !ok {
			continue
		}
		ts, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			continue
		}
		timeline.AddEvent(ts)
	}
}
