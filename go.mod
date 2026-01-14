module github.com/codeGROOVE-dev/best-reviewer

go 1.25.4

require github.com/golang-jwt/jwt/v5 v5.3.0

require (
	github.com/codeGROOVE-dev/fido v1.10.0
	github.com/codeGROOVE-dev/fido/pkg/store/localfs v1.10.0
	github.com/codeGROOVE-dev/fido/pkg/store/null v1.10.0
	github.com/codeGROOVE-dev/gsm v0.0.0-20251019065141-833fe2363d22
	github.com/codeGROOVE-dev/prx v0.0.0-20260106161606-c98f36600fa9
	github.com/codeGROOVE-dev/retry v1.3.1
	github.com/codeGROOVE-dev/sprinkler v0.0.0-20260107012903-de12e2b7508a
)

require (
	github.com/codeGROOVE-dev/fido/pkg/store/compress v1.10.0 // indirect
	github.com/klauspost/compress v1.18.2 // indirect
	github.com/puzpuzpuz/xsync/v4 v4.3.0 // indirect
	golang.org/x/net v0.49.0 // indirect
)
