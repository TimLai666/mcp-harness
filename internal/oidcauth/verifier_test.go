package oidcauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVerifierValidatesAndRejects(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	var issuer string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Discovery{
			Issuer:                issuer,
			AuthorizationEndpoint: issuer + "/auth",
			TokenEndpoint:         issuer + "/token",
			JWKSURI:               issuer + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []jwk{{
			Kty: "RSA", Kid: "test", Alg: "RS256", Use: "sig",
			N: b64.EncodeToString(key.N.Bytes()),
			E: b64.EncodeToString(bigEndianExp(key.E)),
		}}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	issuer = server.URL

	v := NewVerifier(issuer, "mcp-harness")

	good := signJWT(t, key, "test", map[string]any{
		"sub": "user-123", "iss": issuer, "aud": "mcp-harness",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	claims, err := v.Verify(context.Background(), good)
	if err != nil {
		t.Fatalf("expected valid token, got %v", err)
	}
	if claims.Subject != "user-123" {
		t.Fatalf("unexpected subject: %q", claims.Subject)
	}

	expired := signJWT(t, key, "test", map[string]any{
		"sub": "user-123", "iss": issuer, "aud": "mcp-harness",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	if _, err := v.Verify(context.Background(), expired); err == nil {
		t.Fatal("expected expired token to be rejected")
	}

	wrongAud := signJWT(t, key, "test", map[string]any{
		"sub": "user-123", "iss": issuer, "aud": "someone-else",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Verify(context.Background(), wrongAud); err == nil {
		t.Fatal("expected wrong-audience token to be rejected")
	}

	// Tamper with a valid token's payload → signature must fail.
	tampered := good[:len(good)-4] + "AAAA"
	if _, err := v.Verify(context.Background(), tampered); err == nil {
		t.Fatal("expected tampered token to be rejected")
	}
}

func bigEndianExp(e int) []byte {
	b := []byte{byte(e >> 16), byte(e >> 8), byte(e)}
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}

func signJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	signingInput := b64.EncodeToString(h) + "." + b64.EncodeToString(c)
	hashed := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + b64.EncodeToString(sig)
}
