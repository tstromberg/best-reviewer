package activity

import (
	"context"
	"testing"
	"time"
)

func TestTimeline_HoursUntilActive(t *testing.T) {
	tests := []struct {
		name         string
		bucketCounts [24]int
		totalEvents  int
		utcHour      int
		want         int
	}{
		{
			name:         "no events",
			bucketCounts: [24]int{},
			totalEvents:  0,
			utcHour:      12,
			want:         0,
		},
		{
			name: "active this hour",
			bucketCounts: func() [24]int {
				var c [24]int
				c[12] = 10 // 10% of events in hour 12
				return c
			}(),
			totalEvents: 100,
			utcHour:     12,
			want:        1, // active this hour
		},
		{
			name: "active next hour",
			bucketCounts: func() [24]int {
				var c [24]int
				c[13] = 10 // 10% of events in hour 13
				return c
			}(),
			totalEvents: 100,
			utcHour:     12,
			want:        2, // active in 1 hour
		},
		{
			name: "active in 2 hours",
			bucketCounts: func() [24]int {
				var c [24]int
				c[14] = 10 // 10% of events in hour 14
				return c
			}(),
			totalEvents: 100,
			utcHour:     12,
			want:        3, // active in 2 hours
		},
		{
			name: "not active in next 3 hours",
			bucketCounts: func() [24]int {
				var c [24]int
				c[20] = 100 // all events at 8pm
				return c
			}(),
			totalEvents: 100,
			utcHour:     12,
			want:        0, // not active soon
		},
		{
			name: "below threshold not counted",
			bucketCounts: func() [24]int {
				var c [24]int
				c[12] = 4 // only 4% - below 5% threshold
				c[14] = 10
				return c
			}(),
			totalEvents: 100,
			utcHour:     12,
			want:        3, // skips hour 12, finds hour 14
		},
		{
			name: "wraps around midnight",
			bucketCounts: func() [24]int {
				var c [24]int
				c[1] = 10 // 10% at 1am
				return c
			}(),
			totalEvents: 100,
			utcHour:     23,
			want:        3, // 23 -> 0 -> 1 = 2 hours out
		},
		{
			name:         "negative hour returns 0",
			bucketCounts: [24]int{},
			totalEvents:  100,
			utcHour:      -1,
			want:         0,
		},
		{
			name:         "hour 24+ returns 0",
			bucketCounts: [24]int{},
			totalEvents:  100,
			utcHour:      25,
			want:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeline := &Timeline{
				BucketCounts: tt.bucketCounts,
				TotalEvents:  tt.totalEvents,
			}
			got := timeline.HoursUntilActive(tt.utcHour)
			if got != tt.want {
				t.Errorf("HoursUntilActive(%d) = %d, want %d", tt.utcHour, got, tt.want)
			}
		})
	}
}

func TestBucketForHour(t *testing.T) {
	tests := []struct {
		hour int
		want int
	}{
		{0, 0},
		{1, 1},
		{12, 12},
		{23, 23},
		{-1, 0},  // negative clamped to 0
		{25, 23}, // overflow clamped to 23
	}

	for _, tt := range tests {
		got := BucketForHour(tt.hour)
		if got != tt.want {
			t.Errorf("BucketForHour(%d) = %d, want %d", tt.hour, got, tt.want)
		}
	}
}

func TestTimeline_AddEvent(t *testing.T) {
	timeline := &Timeline{}

	// Add events at different hours
	timeline.AddEvent(time.Date(2024, 1, 1, 1, 30, 0, 0, time.UTC))  // hour 1
	timeline.AddEvent(time.Date(2024, 1, 1, 4, 0, 0, 0, time.UTC))   // hour 4
	timeline.AddEvent(time.Date(2024, 1, 1, 13, 45, 0, 0, time.UTC)) // hour 13
	timeline.AddEvent(time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC))  // hour 14
	timeline.AddEvent(time.Date(2024, 1, 1, 22, 30, 0, 0, time.UTC)) // hour 22

	if timeline.TotalEvents != 5 {
		t.Errorf("TotalEvents = %d, want 5", timeline.TotalEvents)
	}
	if timeline.BucketCounts[1] != 1 {
		t.Errorf("BucketCounts[1] = %d, want 1", timeline.BucketCounts[1])
	}
	if timeline.BucketCounts[4] != 1 {
		t.Errorf("BucketCounts[4] = %d, want 1", timeline.BucketCounts[4])
	}
	if timeline.BucketCounts[13] != 1 {
		t.Errorf("BucketCounts[13] = %d, want 1", timeline.BucketCounts[13])
	}
	if timeline.BucketCounts[14] != 1 {
		t.Errorf("BucketCounts[14] = %d, want 1", timeline.BucketCounts[14])
	}
	if timeline.BucketCounts[22] != 1 {
		t.Errorf("BucketCounts[22] = %d, want 1", timeline.BucketCounts[22])
	}
}

func TestTimingBoost(t *testing.T) {
	tests := []struct {
		name      string
		timeline  *Timeline
		baseScore int
		want      int
	}{
		{
			name:      "nil timeline",
			timeline:  nil,
			baseScore: 100,
			want:      0,
		},
		{
			name:      "zero base score",
			timeline:  &Timeline{TotalEvents: 10},
			baseScore: 0,
			want:      0,
		},
		{
			name: "active this hour gives +30%",
			timeline: &Timeline{
				BucketCounts: func() [24]int {
					var counts [24]int
					hour := time.Now().UTC().Hour()
					counts[hour] = 10
					return counts
				}(),
				TotalEvents: 100,
			},
			baseScore: 100,
			want:      30,
		},
		{
			name: "active next hour gives +20%",
			timeline: &Timeline{
				BucketCounts: func() [24]int {
					var counts [24]int
					hour := time.Now().UTC().Hour()
					counts[(hour+1)%24] = 10
					return counts
				}(),
				TotalEvents: 100,
			},
			baseScore: 100,
			want:      20,
		},
		{
			name: "active in 2 hours gives +10%",
			timeline: &Timeline{
				BucketCounts: func() [24]int {
					var counts [24]int
					hour := time.Now().UTC().Hour()
					counts[(hour+2)%24] = 10
					return counts
				}(),
				TotalEvents: 100,
			},
			baseScore: 100,
			want:      10,
		},
		{
			name: "not active in next 3 hours gives -25%",
			timeline: &Timeline{
				BucketCounts: func() [24]int {
					var counts [24]int
					hour := time.Now().UTC().Hour()
					counts[(hour+12)%24] = 100 // 12 hours away
					return counts
				}(),
				TotalEvents: 100,
			},
			baseScore: 100,
			want:      -25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TimingBoost(tt.timeline, tt.baseScore)
			if got != tt.want {
				t.Errorf("TimingBoost() = %d, want %d", got, tt.want)
			}
		})
	}
}

// mockFetcher implements Fetcher for testing.
type mockFetcher struct {
	timelines map[string]*Timeline
	err       error
}

func (m *mockFetcher) FetchTimeline(_ context.Context, _, username string) (*Timeline, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.timelines[username], nil
}

func TestManager_Timeline(t *testing.T) {
	fetcher := &mockFetcher{
		timelines: map[string]*Timeline{
			"alice": {
				Username:    "alice",
				TotalEvents: 50,
				BucketCounts: func() [24]int {
					var c [24]int
					c[9], c[10], c[14], c[15], c[16] = 10, 10, 10, 10, 10
					return c
				}(),
			},
		},
	}

	mgr := NewManager(fetcher, ManagerConfig{CacheTTL: time.Hour})

	// First call should fetch
	timeline := mgr.Timeline(context.Background(), "test-org", "alice")
	if timeline == nil {
		t.Fatal("Expected timeline, got nil")
	}
	if timeline.Username != "alice" {
		t.Errorf("Username = %s, want alice", timeline.Username)
	}
	if timeline.TotalEvents != 50 {
		t.Errorf("TotalEvents = %d, want 50", timeline.TotalEvents)
	}

	// Second call should use cache
	timeline2 := mgr.Timeline(context.Background(), "test-org", "alice")
	if timeline2 != timeline {
		t.Error("Expected cached timeline, got different object")
	}

	// Unknown user returns nil
	unknown := mgr.Timeline(context.Background(), "test-org", "unknown")
	if unknown != nil {
		t.Errorf("Expected nil for unknown user, got %+v", unknown)
	}
}

func TestManager_Timelines(t *testing.T) {
	fetcher := &mockFetcher{
		timelines: map[string]*Timeline{
			"alice": {Username: "alice", TotalEvents: 50},
			"bob":   {Username: "bob", TotalEvents: 100},
		},
	}

	mgr := NewManager(fetcher, ManagerConfig{CacheTTL: time.Hour})

	results := mgr.Timelines(context.Background(), "test-org", []string{"alice", "bob", "charlie"})

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}
	if results["alice"] == nil {
		t.Error("Expected alice timeline")
	}
	if results["bob"] == nil {
		t.Error("Expected bob timeline")
	}
	if results["charlie"] != nil {
		t.Error("Expected nil for charlie")
	}
}
