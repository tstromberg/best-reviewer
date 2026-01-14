// Package config provides per-organization configuration management.
package config

import "time"

// Default values for organization configuration.
const (
	// DefaultMinGracePeriod is the minimum wait after PR update before assigning (minutes).
	DefaultMinGracePeriod = 2
	// DefaultPendingTestGrace is the wait time if tests are pending/queued/running (minutes).
	DefaultPendingTestGrace = 20
	// DefaultFailingTestGrace is the wait time if tests are failing (minutes).
	DefaultFailingTestGrace = 90

	// DefaultMaxReviewers is the maximum number of reviewers to assign per PR.
	DefaultMaxReviewers = 2
	// DefaultMaxPRsPerReviewer is the maximum open PRs per reviewer (workload cap).
	DefaultMaxPRsPerReviewer = 9

	// DefaultMinAge is the minimum PR age before assignment (hours).
	DefaultMinAge = 0
	// DefaultMaxAge is the maximum PR age to consider (hours), default 1 year.
	DefaultMaxAge = 365 * 24

	// ConfigCacheTTL is the TTL for cached org configs.
	ConfigCacheTTL = 30 * time.Minute
	// ConfigRepoName is the repository containing config files.
	ConfigRepoName = ".codeGROOVE"
	// ConfigFileName is the config file name within repo.
	ConfigFileName = "assigner.yaml"
)
