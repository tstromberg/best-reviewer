// Package activity provides user activity timeline analysis for reviewer timing boost.
package activity

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/codeGROOVE-dev/fido"
)

// BucketCount is the number of 1-hour buckets per day.
const BucketCount = 24

// ActivityThreshold is the minimum fraction of activity in an hour to consider it "active".
const ActivityThreshold = 0.05 // 5% of total events

// Timeline represents a user's activity distribution across 1-hour UTC time buckets.
// Each bucket represents one hour: 0=00:00-01:00, 1=01:00-02:00, ..., 23=23:00-24:00.
type Timeline struct {
	FetchedAt    time.Time
	Username     string
	BucketCounts [BucketCount]int
	TotalEvents  int
}

// HoursUntilActive returns how many hours until the user is likely active (1-3),
// or 0 if they have no significant activity in the next 3 hours.
// An hour is considered "active" if it has ≥5% of the user's total activity.
func (t *Timeline) HoursUntilActive(utcHour int) int {
	if t.TotalEvents == 0 || utcHour < 0 || utcHour >= 24 {
		return 0
	}

	// Use ceiling to properly enforce 5% threshold (e.g., 171 events requires 9 events, not 8)
	threshold := max(1, int(math.Ceil(float64(t.TotalEvents)*ActivityThreshold)))

	// Check next 3 hours
	for i := range 3 {
		bucket := (utcHour + i) % 24
		if t.BucketCounts[bucket] >= threshold {
			return i + 1 // 1 = this hour, 2 = next hour, 3 = two hours out
		}
	}
	return 0 // Not active in next 3 hours
}

// BucketForHour returns the bucket index (0-23) for a given UTC hour.
func BucketForHour(utcHour int) int {
	if utcHour < 0 {
		return 0
	}
	if utcHour >= BucketCount {
		return BucketCount - 1
	}
	return utcHour
}

// AddEvent increments the bucket count for the given timestamp.
func (t *Timeline) AddEvent(ts time.Time) {
	bucket := BucketForHour(ts.UTC().Hour())
	t.BucketCounts[bucket]++
	t.TotalEvents++
}

// Fetcher fetches activity timelines for users within an organization.
type Fetcher interface {
	FetchTimeline(ctx context.Context, org, username string) (*Timeline, error)
}

// Manager manages activity timeline fetching and caching.
type Manager struct {
	fetcher Fetcher
	cache   *fido.Cache[string, *Timeline]
	mu      sync.RWMutex
}

// ManagerConfig configures the Manager.
type ManagerConfig struct {
	CacheTTL time.Duration // Default: 15 days
}

// NewManager creates a new activity timeline manager.
func NewManager(fetcher Fetcher, cfg ManagerConfig) *Manager {
	ttl := cfg.CacheTTL
	if ttl == 0 {
		ttl = 15 * 24 * time.Hour // Default: 15 days since activity patterns change slowly
	}
	return &Manager{
		fetcher: fetcher,
		cache:   fido.New[string, *Timeline](fido.TTL(ttl)),
	}
}

// Timeline returns the activity timeline for a user within an org, using cache if available.
func (m *Manager) Timeline(ctx context.Context, org, username string) *Timeline {
	key := "activity:" + org + ":" + username

	m.mu.RLock()
	if t, ok := m.cache.Get(key); ok {
		m.mu.RUnlock()
		return t
	}
	m.mu.RUnlock()

	timeline, err := m.fetcher.FetchTimeline(ctx, org, username)
	if err != nil {
		slog.Warn("Failed to fetch activity timeline", "org", org, "user", username, "error", err)
		return nil
	}

	m.mu.Lock()
	m.cache.Set(key, timeline)
	m.mu.Unlock()

	return timeline
}

// Timelines returns activity timelines for multiple users within an org in batch.
// Fetches timelines in parallel for better performance.
// Returns a map of username to timeline; missing users have nil values.
func (m *Manager) Timelines(ctx context.Context, org string, usernames []string) map[string]*Timeline {
	result := make(map[string]*Timeline, len(usernames))
	if len(usernames) == 0 {
		return result
	}

	// Fetch all timelines in parallel
	type fetchResult struct {
		timeline *Timeline
		username string
	}
	results := make(chan fetchResult, len(usernames))

	for _, username := range usernames {
		go func(u string) {
			results <- fetchResult{username: u, timeline: m.Timeline(ctx, org, u)}
		}(username)
	}

	// Collect results
	for range usernames {
		r := <-results
		result[r.username] = r.timeline
	}

	return result
}

// TimingBoost calculates the score adjustment for a candidate based on their
// expected availability in the next few hours.
// Returns: +30% for active this hour, +20% for next hour, +10% for 2 hours out,
// -25% penalty if not active in next 3 hours (likely asleep/unavailable).
func TimingBoost(timeline *Timeline, baseScore int) int {
	if timeline == nil || baseScore <= 0 {
		return 0
	}

	utcHour := time.Now().UTC().Hour()
	hoursUntil := timeline.HoursUntilActive(utcHour)

	// Log timeline details for debugging
	slog.Debug("Timing boost calculation",
		"user", timeline.Username,
		"utc_hour", utcHour,
		"hours_until_active", hoursUntil,
		"total_events", timeline.TotalEvents,
		"bucket_counts", timeline.BucketCounts,
		"base_score", baseScore)

	// Boost/penalty based on hours until active
	var multiplier float64
	switch hoursUntil {
	case 1:
		multiplier = 0.30 // Active this hour: +30%
	case 2:
		multiplier = 0.20 // Active next hour: +20%
	case 3:
		multiplier = 0.10 // Active in 2 hours: +10%
	default:
		multiplier = -0.25 // Not active in next 3 hours: -25% penalty
	}

	return int(float64(baseScore) * multiplier)
}
