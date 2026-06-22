package harness

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"strings"
)

const (
	githubTokenSetting = "github_token"
	githubLoginSetting = "github_login"
)

// SetGitHubToken stores a tenant's GitHub OAuth token (encrypted at rest) and
// the linked account login. Tokens live in the tenant's isolated store, so each
// owner's GitHub connection is private to them.
func SetGitHubToken(owner, token, login string) error {
	store, err := DefaultStoreFor(owner)
	if err != nil {
		return err
	}
	enc, err := encryptSecret(token)
	if err != nil {
		return err
	}
	if err := store.SetSetting(githubTokenSetting, enc); err != nil {
		return err
	}
	store, err = DefaultStoreFor(owner)
	if err != nil {
		return err
	}
	return store.SetSetting(githubLoginSetting, login)
}

// GetGitHubToken returns a tenant's decrypted GitHub token and account login.
func GetGitHubToken(owner string) (token, login string, ok bool) {
	store, err := DefaultStoreFor(owner)
	if err != nil {
		return "", "", false
	}
	enc, present, err := store.GetSetting(githubTokenSetting)
	if err != nil || !present || enc == "" {
		return "", "", false
	}
	plain, err := decryptSecret(enc)
	if err != nil || plain == "" {
		return "", "", false
	}
	store, err = DefaultStoreFor(owner)
	if err == nil {
		login, _, _ = store.GetSetting(githubLoginSetting)
	}
	return plain, login, true
}

// GitHubLogin returns just the linked account login (no token), for status display.
func GitHubLogin(owner string) (string, bool) {
	_, login, ok := GetGitHubToken(owner)
	return login, ok
}

// ClearGitHubToken removes a tenant's GitHub connection.
func ClearGitHubToken(owner string) error {
	store, err := DefaultStoreFor(owner)
	if err != nil {
		return err
	}
	if err := store.SetSetting(githubTokenSetting, ""); err != nil {
		return err
	}
	store, err = DefaultStoreFor(owner)
	if err != nil {
		return err
	}
	return store.SetSetting(githubLoginSetting, "")
}

// AppendGitHubEnv returns base augmented with the owner's GitHub credentials so
// that git over https://github.com and the gh CLI authenticate as that user. It
// injects the token via git's env-based config (GIT_CONFIG_*) so it is never
// written to any .git/config or credential file. With no token it returns base
// unchanged.
func AppendGitHubEnv(base []string, owner string) []string {
	token, login, ok := GetGitHubToken(owner)
	if !ok || token == "" {
		log.Printf("[github] no token found for owner=%q — git will not authenticate", owner)
		return base
	}
	log.Printf("[github] injecting credentials for owner=%q login=%s", owner, login)
	drop := map[string]bool{
		"GIT_CONFIG_COUNT": true,
		"GIT_CONFIG_KEY_0": true, "GIT_CONFIG_VALUE_0": true,
		"GIT_CONFIG_KEY_1": true, "GIT_CONFIG_VALUE_1": true,
		"GIT_TERMINAL_PROMPT": true,
		"GH_TOKEN": true, "GITHUB_TOKEN": true,
	}
	out := make([]string, 0, len(base)+8)
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		if !drop[key] {
			out = append(out, kv)
		}
	}
	out = append(out,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=url.https://x-access-token:"+token+"@github.com/.insteadOf",
		"GIT_CONFIG_VALUE_0=https://github.com/",
		"GIT_CONFIG_KEY_1=credential.helper",
		"GIT_CONFIG_VALUE_1=",
		"GH_TOKEN="+token,
		"GITHUB_TOKEN="+token,
	)
	return out
}

func encryptionKey() [32]byte {
	raw := os.Getenv("MCP_HARNESS_SESSION_SECRET")
	if raw == "" {
		raw = "mcp-harness-default-key"
	}
	return sha256.Sum256([]byte(raw))
}

func encryptSecret(plaintext string) (string, error) {
	key := encryptionKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptSecret(encoded string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	key := encryptionKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
