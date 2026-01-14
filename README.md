# Better Reviewers

Automated PR reviewer assignment based on git blame analysis and contributor workload.

## Build

```bash
go build -o better-reviewers ./cmd/best-reviewer-bot
```

## Authentication

### GitHub App (Production)

```bash
export GITHUB_APP_ID="123456"
export GITHUB_APP_KEY="projects/PROJECT/secrets/SECRET/versions/latest"  # GCP Secret Manager
# or
export GITHUB_APP_KEY_PATH="/path/to/private-key.pem"  # Local file
```

Required App permissions: `repository:write`, `pull_requests:write`, `organization_members:read`

### GitHub CLI (Development)

```bash
gh auth login
```

## Usage

```bash
# Single PR
./better-reviewers -pr "owner/repo#123"

# Project monitoring
./better-reviewers -project "owner/repo" -poll 1h

# Organization monitoring
./better-reviewers -org "myorg" -poll 1h

# GitHub App mode (monitors all installed orgs)
./better-reviewers -poll 1h

# Dry run
./better-reviewers -pr "owner/repo#123" -dry-run
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-pr` | | PR URL or `owner/repo#N` |
| `-project` | | Repository to monitor |
| `-org` | | Organization to monitor |
| `--app-id` | | GitHub App ID (overrides env) |
| `--app-key` | | App private key path (overrides env) |
| `-poll` | | Polling interval (e.g. `1h`, `30m`) |
| `-dry-run` | `false` | Preview without assigning |
| `-min-age` | `1h` | Minimum PR age |
| `-max-age` | `180d` | Maximum PR age |
| `-max-prs` | `9` | Max open PRs per reviewer |
| `-pr-count-cache` | `6h` | PR count cache TTL |

## Per-Org Configuration

Create `.codeGROOVE/assigner.yaml` in any org repository:

```yaml
min_grace_period: 2        # Minutes before assignment
pending_test_grace: 20     # Minutes while tests pending
failing_test_grace: 90     # Minutes while tests failing
max_reviewers: 2
max_prs_per_reviewer: 9
min_age: 0                 # Hours
max_age: 8760              # Hours (1 year)

excluded_users:
  - bot-account

excluded_paths:
  - "vendor/**"
  - "**/*.generated.go"
```

## Algorithm

1. Fetch changed files from PR
2. Run `git blame` on each file to identify contributors
3. Score candidates by: code ownership weight, recent activity, current workload
4. Filter out: PR author, bots, overloaded reviewers (>max_prs open)
5. Assign top N reviewers

## Deployment

### Cloud Run

```bash
gcloud run deploy better-reviewers \
  --image gcr.io/PROJECT/better-reviewers \
  --set-env-vars GITHUB_APP_ID=123456 \
  --set-secrets GITHUB_APP_KEY=github-app-key:latest
```

### Kubernetes

```yaml
env:
  - name: GITHUB_APP_ID
    value: "123456"
  - name: GITHUB_APP_KEY
    valueFrom:
      secretKeyRef:
        name: github-app-key
        key: private-key
```
