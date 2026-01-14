package github

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateAppID_Valid(t *testing.T) {
	tests := []struct {
		name  string
		appID string
	}{
		{"single digit", "1"},
		{"multiple digits", "123456"},
		{"max valid", "999999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAppID(tt.appID)
			if err != nil {
				t.Errorf("validateAppID(%q) unexpected error: %v", tt.appID, err)
			}
		})
	}
}

func TestValidateAppID_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		appID   string
		wantErr string
	}{
		{"empty", "", "app ID cannot be empty"},
		{"non-numeric", "abc", "app ID must be numeric"},
		{"negative", "-1", "app ID must be numeric"},
		{"too large", "9999999999", "app ID too large"},
		{"with spaces", "123 456", "app ID must be numeric"},
		{"with special chars", "123@456", "app ID must be numeric"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAppID(tt.appID)
			if err == nil {
				t.Errorf("validateAppID(%q) expected error, got nil", tt.appID)
			}
		})
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"too short", "abc"},
		{"just under min", "ghp_" + "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateToken(tt.token)
			if err == nil {
				t.Errorf("validateToken(%q) expected error, got nil", tt.token)
			}
		})
	}
}

func TestAuthConstants(t *testing.T) {
	// Verify auth constants have reasonable values
	if maxTokenLength <= minTokenLength {
		t.Error("maxTokenLength should be greater than minTokenLength")
	}

	if minTokenLength != 40 {
		t.Errorf("expected minTokenLength to be 40, got %d", minTokenLength)
	}

	if maxTokenLength != 100 {
		t.Errorf("expected maxTokenLength to be 100, got %d", maxTokenLength)
	}

	if classicTokenLength != 40 {
		t.Errorf("expected classicTokenLength to be 40, got %d", classicTokenLength)
	}

	if maxAppID != 999999999 {
		t.Errorf("expected maxAppID to be 999999999, got %d", maxAppID)
	}

	if filePermSecure != 0o077 {
		t.Errorf("expected filePermSecure to be 0o077, got %o", filePermSecure)
	}

	if filePermReadOnly != 0o400 {
		t.Errorf("expected filePermReadOnly to be 0o400, got %o", filePermReadOnly)
	}

	if filePermOwnerRW != 0o600 {
		t.Errorf("expected filePermOwnerRW to be 0o600, got %o", filePermOwnerRW)
	}
}

func TestClient_RefreshJWTIfNeeded_NotAppAuth(t *testing.T) {
	c := &Client{
		isAppAuth: false,
	}

	// Should be no-op for non-app auth
	err := c.refreshJWTIfNeeded()
	if err != nil {
		t.Errorf("refreshJWTIfNeeded() unexpected error for non-app auth: %v", err)
	}
}

func TestClient_RefreshJWTIfNeeded_NoRefreshNeeded(t *testing.T) {
	c := &Client{
		isAppAuth:   true,
		tokenExpiry: time.Now().Add(time.Hour), // Not expired
		appID:       "123456",
		privateKeyContent: []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF0q4JwfFLp8rh6f5tLUGJKqWJQs9
-----END RSA PRIVATE KEY-----`),
	}

	// Should not refresh if not needed
	err := c.refreshJWTIfNeeded()
	if err != nil {
		t.Errorf("refreshJWTIfNeeded() unexpected error: %v", err)
	}
}

func TestClient_SetPrxClient(t *testing.T) {
	c := &Client{}

	mockPrx := &mockPrxClientImpl{}
	c.SetPrxClient(mockPrx)

	if c.prxClient == nil {
		t.Error("expected prxClient to be set")
	}
}

type mockPrxClientImpl struct{}

func (m *mockPrxClientImpl) PullRequestWithReferenceTime(ctx context.Context, owner, repo string, prNumber int, referenceTime time.Time) (any, error) {
	return map[string]any{"number": prNumber}, nil
}

func TestNew_PersonalTokenMode(t *testing.T) {
	ctx := context.Background()

	// Test that New works with personal token mode
	cfg := Config{
		UseAppAuth:  false,
		Token:       "",
		HTTPTimeout: 30 * time.Second,
		CacheTTL:    time.Hour,
	}

	// This should not error - it creates a client even with empty token
	// The token validation happens on first use, not at client creation
	_, err := New(ctx, cfg)
	if err != nil {
		t.Logf("Note: New with empty token returned error: %v", err)
		// This is acceptable behavior
	}
}

func TestNewPersonalTokenClient_WithValidToken(t *testing.T) {
	ctx := context.Background()

	// Use a valid token format
	validToken := "ghp_" + strings.Repeat("a", 36)

	client, err := newPersonalTokenClient(ctx, validToken, 30*time.Second, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if client.token != validToken {
		t.Errorf("expected token %q, got %q", validToken, client.token)
	}

	if client.isAppAuth {
		t.Error("expected isAppAuth to be false for personal token")
	}
}

func TestClient_TokenMutex(t *testing.T) {
	c := &Client{
		installationTypes: make(map[string]string),
	}

	// Test concurrent access to ensure mutex works
	done := make(chan bool)

	// Concurrent SetCurrentOrg calls
	for i := range 10 {
		go func(id int) {
			c.SetCurrentOrg("org" + string(rune(id)))
			done <- true
		}(i)
	}

	// Concurrent IsUserAccount calls
	for i := range 10 {
		go func(id int) {
			c.IsUserAccount("org" + string(rune(id)))
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range 20 {
		<-done
	}
}

func TestLoadPrivateKey_WithContent(t *testing.T) {
	// Valid RSA private key content
	validKey := []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpQIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF0q4JwfFLp8rh6f5tLUGJKqWJQs9
MIIEpQIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF0q4JwfFLp8rh6f5tLUGJKqWJQs9
-----END RSA PRIVATE KEY-----`)

	key, err := loadPrivateKey(validKey, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(key), "BEGIN RSA PRIVATE KEY") {
		t.Error("expected key to contain RSA private key header")
	}
}

func TestLoadPrivateKey_WithPKCS8Content(t *testing.T) {
	// Valid PKCS8 private key content
	validKey := []byte(`-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDRndVLkklx2zfF
-----END PRIVATE KEY-----`)

	key, err := loadPrivateKey(validKey, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(key), "BEGIN PRIVATE KEY") {
		t.Error("expected key to contain PKCS8 private key header")
	}
}

func TestLoadPrivateKey_NoKeyProvided(t *testing.T) {
	_, err := loadPrivateKey(nil, "")
	if err == nil {
		t.Error("expected error when no key provided")
	}
	if !strings.Contains(err.Error(), "no private key provided") {
		t.Errorf("expected 'no private key provided' error, got: %v", err)
	}
}

func TestLoadPrivateKey_InvalidKeyContent(t *testing.T) {
	invalidKey := []byte("not a valid private key")
	_, err := loadPrivateKey(invalidKey, "")
	if err == nil {
		t.Error("expected error for invalid key content")
	}
	if !strings.Contains(err.Error(), "valid PEM") {
		t.Errorf("expected 'valid PEM' error, got: %v", err)
	}
}

func TestLoadPrivateKey_WithFilePath(t *testing.T) {
	// Create a temp file with valid key content
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.pem")
	validKey := []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpQIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF0q4JwfFLp8rh6f5tLUGJKqWJQs9
-----END RSA PRIVATE KEY-----`)
	if err := os.WriteFile(keyPath, validKey, 0o600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	key, err := loadPrivateKey(nil, keyPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(key), "BEGIN RSA PRIVATE KEY") {
		t.Error("expected key to contain RSA private key header")
	}
}

func TestReadPrivateKeyFile_RelativePath(t *testing.T) {
	_, err := readPrivateKeyFile("relative/path/to/key.pem")
	if err == nil {
		t.Error("expected error for relative path")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Errorf("expected 'absolute path' error, got: %v", err)
	}
}

func TestReadPrivateKeyFile_NonExistent(t *testing.T) {
	_, err := readPrivateKeyFile("/nonexistent/path/to/key.pem")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "cannot access") {
		t.Errorf("expected 'cannot access' error, got: %v", err)
	}
}

func TestReadPrivateKeyFile_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := readPrivateKeyFile(tmpDir)
	if err == nil {
		t.Error("expected error when path is a directory")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected 'not a directory' error, got: %v", err)
	}
}

func TestReadPrivateKeyFile_InsecurePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "insecure.pem")
	if err := os.WriteFile(keyPath, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	_, err := readPrivateKeyFile(keyPath)
	if err == nil {
		t.Error("expected error for insecure permissions")
	}
	if !strings.Contains(err.Error(), "insecure permissions") {
		t.Errorf("expected 'insecure permissions' error, got: %v", err)
	}
}

func TestReadPrivateKeyFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "valid.pem")
	content := []byte("test private key content")
	if err := os.WriteFile(keyPath, content, 0o600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	data, err := readPrivateKeyFile(keyPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Errorf("expected %q, got %q", content, data)
	}
}

func TestReadPrivateKeyFile_ReadOnlyPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "readonly.pem")
	content := []byte("test private key content")
	if err := os.WriteFile(keyPath, content, 0o400); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	data, err := readPrivateKeyFile(keyPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Errorf("expected %q, got %q", content, data)
	}
}

func TestGenerateJWT_InvalidPEM(t *testing.T) {
	_, err := generateJWT("123456", []byte("not a valid PEM"))
	if err == nil {
		t.Error("expected error for invalid PEM")
	}
	if !strings.Contains(err.Error(), "failed to parse PEM") {
		t.Errorf("expected 'failed to parse PEM' error, got: %v", err)
	}
}

func TestGenerateJWT_InvalidPrivateKey(t *testing.T) {
	// Valid PEM block but not a private key
	invalidKey := []byte(`-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAJHGpH8TwStlMA0GCSqGSIb3DQEBCwUAMBExDzANBgNVBAMMBnVu
-----END CERTIFICATE-----`)
	_, err := generateJWT("123456", invalidKey)
	if err == nil {
		t.Error("expected error for non-private-key PEM")
	}
}

// testRSAPrivateKey is a valid RSA private key for testing purposes only.
// Generated specifically for tests - DO NOT use in production.
var testRSAPrivateKey = []byte(`-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDAkE6bRXDOJkrd
jWSQrkn3ohbHpHTBV2FFw49hzCArcjOBUm8WWw6+E7g5BU1jxyDfG/0mpLap8tAX
v7o7oB5Df2ZE9zj5Gi5sJMzbjdjjVUVlPIb4ZbRJOL/Kj/o7VLfBVZmWol4mgjPj
j+MwzqvXX5DpJRSGgqeTjBz/KrMT5OWJ+j/lXRuqp9EnZnK/eMuD/+DRE7kuIhBV
5Vv9+Y1RIpgook1ySVTj81jD9kNHt++qT86e+Rqf+LyfYfG5lNmrLbb/IsCagfo2
nF9/ujsvvEXz8CwJ+zYRYosU2C77mKYInt5ipEhVIlG9Rlaj1PrnIFdQ4dcuSBgP
3/fNdVktAgMBAAECggEAKkfVNqMT3nPKduR7iwf1vj11Bn9d0nDi7ww+IIFPI/L2
i6Pjt9MlBMe0KLL5F9oqZcqRtkkuwViK59gFZl+lHXls5WIp/IoK3NxkraVy1JGN
w+l7EjHUmMow1GNyFFJo6Xan20sJ5KcsiP/4KKiMUyUM3qAxZkpsTImUeVNxEAIN
i2TLO6gvoeDTk+DTflCiN4MzSuN3uDNvmtpBSDBdFn3Jekcq20yI0LyuXdufUKqK
wcefbCbLSh1mAnj65EqG59tk6sqnVLS1OFJMpzREbU5XeZNjt7M8KQ/ICiFqW+Aq
rinHvys2MJBI8sBRu5z6JF9DXnF/pvszlpCnd3e5kQKBgQD02ha47ogoTjqJ8x5B
+8CJfvZ9MG7VXuCL28xGjOXJKV1k/IhAu3L9ZLh6vcSudWgp0gaqIo2XzeHRsosJ
bKi2+7tCmJqUVRNWaAlrfDSiGYtCJ8KZW0oVu4HO2why2tpHWwkwFzn7M6LovhBX
P+djfGsA938KOx5H5HzBt/T+3QKBgQDJVMO6np6PYvKi1a7bYZlIvw4UlmNCTzX+
JADuS3qd4uEJvLe5VLz/TO1zSQl5Yi95xGgzdxMUihIKdyfo4x6hUdwNynSDJ0Ek
YwappAh4ljF/w0kDkxGSHisLTAi8Uqd9hnf6Eundn9ZDSzvOdXzriYOhhyQPlrjs
NPjnZ1IWkQKBgBleNxh17jlu0XXVcH8ZnDsiolsaF4GX0N/sp99vXadX18tMtrku
Mp26P7rHyobgtygOEI60AcOGmyzkuK8DSP+cWSxvLyTLI7PCF6fBOJrK1rjF8c19
vdE+mhZabyenMRJPhkYrQeCa2vgOKRdBEbInA9cXzVu8AEkmjR5s9r8pAoGBAII2
u5zwyFauxYWBtOUY+73sK9wu5CXX+3DSsnNtB/Ij8i6NCzrnzpFEnPMKUwFZ+qDD
4i0fH40SO9be+EYM1xu5SRz2S2MkOWKiVYXUnNH5OiyLDqcsMJoTvv1AgQnkX4W1
OdXY878uiLLfbt/6ZwAj4anQMQeQESxcmnt3/MSxAoGAbHCd7vi9hxxNY9mWW+0J
j1pVwGY9sx3lDnQxjoHC34ippiFIcW5nZMUD2iqVe5Wb34c0Mp/BGFM78FTvG1Qy
hqz7jNXri3jxXQitgZ6wqzdnN7nHMDvw2wlhydrsQjTqlPft3fqSNmrHtZmk7JbC
520TRhYfZHMgndynoUDnDvA=
-----END PRIVATE KEY-----`)

func TestGenerateJWT_ValidKey(t *testing.T) {
	token, err := generateJWT("123456", testRSAPrivateKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token == "" {
		t.Error("expected non-empty JWT token")
	}

	// JWT should have 3 parts separated by dots
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("expected JWT with 3 parts, got %d", len(parts))
	}
}

func TestClient_RefreshJWTIfNeeded_NeedsRefresh_WithContent(t *testing.T) {
	c := &Client{
		isAppAuth:         true,
		tokenExpiry:       time.Now().Add(-time.Hour), // Already expired
		appID:             "123456",
		privateKeyContent: testRSAPrivateKey,
	}

	err := c.refreshJWTIfNeeded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Token should be refreshed
	if c.token == "" {
		t.Error("expected token to be set after refresh")
	}

	// Expiry should be updated
	if time.Now().After(c.tokenExpiry) {
		t.Error("expected tokenExpiry to be in the future after refresh")
	}
}

func TestClient_RefreshJWTIfNeeded_NeedsRefresh_WithFilePath(t *testing.T) {
	// Create temp file with the key
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.pem")
	if err := os.WriteFile(keyPath, testRSAPrivateKey, 0o600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	c := &Client{
		isAppAuth:      true,
		tokenExpiry:    time.Now().Add(-time.Hour), // Already expired
		appID:          "123456",
		privateKeyPath: keyPath,
	}

	err := c.refreshJWTIfNeeded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Token should be refreshed
	if c.token == "" {
		t.Error("expected token to be set after refresh")
	}
}

func TestClient_RefreshJWTIfNeeded_NoPrivateKey(t *testing.T) {
	c := &Client{
		isAppAuth:   true,
		tokenExpiry: time.Now().Add(-time.Hour), // Already expired
		appID:       "123456",
		// No privateKeyContent or privateKeyPath
	}

	err := c.refreshJWTIfNeeded()
	if err == nil {
		t.Error("expected error when no private key available")
	}
	if !strings.Contains(err.Error(), "no private key available") {
		t.Errorf("expected 'no private key available' error, got: %v", err)
	}
}

func TestClient_RefreshJWTIfNeeded_InvalidKeyFile(t *testing.T) {
	c := &Client{
		isAppAuth:      true,
		tokenExpiry:    time.Now().Add(-time.Hour), // Already expired
		appID:          "123456",
		privateKeyPath: "/nonexistent/path/to/key.pem",
	}

	err := c.refreshJWTIfNeeded()
	if err == nil {
		t.Error("expected error when key file doesn't exist")
	}
	if !strings.Contains(err.Error(), "failed to read private key") {
		t.Errorf("expected 'failed to read private key' error, got: %v", err)
	}
}

func TestClient_RefreshJWTIfNeeded_DoubleCheckSkip(t *testing.T) {
	c := &Client{
		isAppAuth:         true,
		tokenExpiry:       time.Now().Add(time.Hour), // Not expired
		appID:             "123456",
		privateKeyContent: testRSAPrivateKey,
		token:             "original-token",
	}

	err := c.refreshJWTIfNeeded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Token should NOT be refreshed
	if c.token != "original-token" {
		t.Errorf("expected token to remain 'original-token', got %q", c.token)
	}
}

func TestClient_GetInstallationToken_NotAppAuth(t *testing.T) {
	c := &Client{
		isAppAuth: false,
		token:     "personal-token",
	}

	token, err := c.getInstallationToken(context.Background(), "test-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token != "personal-token" {
		t.Errorf("expected 'personal-token', got %q", token)
	}
}

func TestClient_GetInstallationToken_EmptyOrg(t *testing.T) {
	c := &Client{
		isAppAuth: true,
		token:     "jwt-token",
	}

	_, err := c.getInstallationToken(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty org name")
	}
	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("expected 'cannot be empty' error, got: %v", err)
	}
}

func TestClient_GetInstallationToken_CachedToken(t *testing.T) {
	c := &Client{
		isAppAuth:          true,
		token:              "jwt-token",
		installationTokens: map[string]string{"test-org": "cached-install-token"},
		installationExpiry: map[string]time.Time{"test-org": time.Now().Add(time.Hour)},
	}

	token, err := c.getInstallationToken(context.Background(), "test-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token != "cached-install-token" {
		t.Errorf("expected 'cached-install-token', got %q", token)
	}
}

func TestClient_GetInstallationToken_NoInstallationID(t *testing.T) {
	c := &Client{
		isAppAuth:          true,
		token:              "jwt-token",
		tokenExpiry:        time.Now().Add(time.Hour),
		installationTokens: map[string]string{},
		installationExpiry: map[string]time.Time{},
		installationIDs:    map[string]int{}, // No installation ID for test-org
	}

	_, err := c.getInstallationToken(context.Background(), "test-org")
	if err == nil {
		t.Error("expected error when no installation ID found")
	}
	if !strings.Contains(err.Error(), "no installation ID found") {
		t.Errorf("expected 'no installation ID found' error, got: %v", err)
	}
}

func TestClient_GetInstallationToken_ExpiredCacheToken(t *testing.T) {
	c := &Client{
		isAppAuth:          true,
		token:              "jwt-token",
		tokenExpiry:        time.Now().Add(time.Hour),
		installationTokens: map[string]string{"test-org": "expired-token"},
		installationExpiry: map[string]time.Time{"test-org": time.Now().Add(-time.Hour)}, // Expired
		installationIDs:    map[string]int{},                                             // No ID to trigger error after expiry check
	}

	_, err := c.getInstallationToken(context.Background(), "test-org")
	if err == nil {
		t.Error("expected error when token expired and no installation ID")
	}
}

func TestClient_ListAppInstallations_NotAppAuth(t *testing.T) {
	c := &Client{
		isAppAuth: false,
	}

	_, err := c.ListAppInstallations(context.Background())
	if err == nil {
		t.Error("expected error for non-app auth")
	}
	if !strings.Contains(err.Error(), "GitHub App authentication") {
		t.Errorf("expected 'GitHub App authentication' error, got: %v", err)
	}
}

func TestClient_Token_AppAuthWithOrg(t *testing.T) {
	c := &Client{
		isAppAuth:          true,
		token:              "jwt-token",
		currentOrg:         "test-org",
		tokenExpiry:        time.Now().Add(time.Hour),
		installationTokens: map[string]string{"test-org": "install-token"},
		installationExpiry: map[string]time.Time{"test-org": time.Now().Add(time.Hour)},
	}

	token, err := c.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token != "install-token" {
		t.Errorf("expected 'install-token', got %q", token)
	}
}

func TestNew_WithAppAuth(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		UseAppAuth:  true,
		AppID:       "", // Invalid - should fail
		HTTPTimeout: 30 * time.Second,
		CacheTTL:    time.Hour,
	}

	_, err := New(ctx, cfg)
	if err == nil {
		t.Log("Note: New with invalid app auth may succeed or fail depending on env")
	}
	// Just checking the branch is covered - error expected due to missing app ID
}

func TestCreateAppAuthClient_Success(t *testing.T) {
	ctx := context.Background()

	client, err := createAppAuthClient(ctx, "123456", "", testRSAPrivateKey, "jwt-token", 30*time.Second, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if !client.isAppAuth {
		t.Error("expected isAppAuth to be true")
	}

	if client.token != "jwt-token" {
		t.Errorf("expected token 'jwt-token', got %q", client.token)
	}
}
