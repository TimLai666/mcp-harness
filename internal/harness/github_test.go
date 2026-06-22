package harness

import (
	"strings"
	"testing"
)

func TestEncryptSecretRoundTrip(t *testing.T) {
	t.Setenv("MCP_HARNESS_SESSION_SECRET", "test-secret-key")
	enc, err := encryptSecret("ghp_supersecret")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "" || strings.Contains(enc, "ghp_supersecret") {
		t.Fatalf("ciphertext must not contain plaintext: %q", enc)
	}
	plain, err := decryptSecret(enc)
	if err != nil || plain != "ghp_supersecret" {
		t.Fatalf("round-trip failed: %q err=%v", plain, err)
	}
	// A different key must fail to decrypt.
	t.Setenv("MCP_HARNESS_SESSION_SECRET", "different-key")
	if _, err := decryptSecret(enc); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestGitHubTokenIsolatedPerTenant(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	t.Setenv("MCP_HARNESS_SESSION_SECRET", "test-secret-key")

	if err := SetGitHubToken("alice", "alice-token", "alice-gh"); err != nil {
		t.Fatal(err)
	}
	if err := SetGitHubToken("bob", "bob-token", "bob-gh"); err != nil {
		t.Fatal(err)
	}

	tok, login, ok := GetGitHubToken("alice")
	if !ok || tok != "alice-token" || login != "alice-gh" {
		t.Fatalf("alice token wrong: %q %q %v", tok, login, ok)
	}
	tok, login, ok = GetGitHubToken("bob")
	if !ok || tok != "bob-token" || login != "bob-gh" {
		t.Fatalf("bob token wrong: %q %q %v", tok, login, ok)
	}
	// A tenant with no connection has no token.
	if _, _, ok := GetGitHubToken("carol"); ok {
		t.Fatal("carol should have no github token")
	}

	// Disconnect clears it.
	if err := ClearGitHubToken("alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := GetGitHubToken("alice"); ok {
		t.Fatal("alice token should be cleared")
	}
	if _, _, ok := GetGitHubToken("bob"); !ok {
		t.Fatal("bob token must survive alice disconnect")
	}
}

func TestAppendGitHubEnvInjectsCredentials(t *testing.T) {
	t.Setenv("MCP_HARNESS_HOME", t.TempDir())
	t.Setenv("MCP_HARNESS_SESSION_SECRET", "test-secret-key")

	base := []string{"PATH=/usr/bin", "GH_TOKEN=stale", "HOME=/home/x"}

	// No token: unchanged.
	got := AppendGitHubEnv(base, "nobody")
	if len(got) != len(base) {
		t.Fatalf("expected unchanged env, got %#v", got)
	}

	if err := SetGitHubToken("alice", "alice-token", "alice-gh"); err != nil {
		t.Fatal(err)
	}
	got = AppendGitHubEnv(base, "alice")
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "GH_TOKEN=alice-token") {
		t.Fatalf("expected GH_TOKEN injected, got %s", joined)
	}
	if strings.Count(joined, "GH_TOKEN=") != 1 {
		t.Fatalf("stale GH_TOKEN must be dropped: %s", joined)
	}
	if !strings.Contains(joined, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader") || !strings.Contains(joined, "GIT_CONFIG_VALUE_0=Authorization: Basic ") {
		t.Fatalf("expected git extraheader config, got %s", joined)
	}
}
