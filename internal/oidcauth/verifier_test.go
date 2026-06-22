package oidcauth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
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

func TestVerifierAcceptsES384(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	var issuer string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Discovery{Issuer: issuer, JWKSURI: issuer + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		x := make([]byte, 48)
		y := make([]byte, 48)
		key.X.FillBytes(x)
		key.Y.FillBytes(y)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []jwk{{
			Kty: "EC", Kid: "ec1", Alg: "ES384", Use: "sig", Crv: "P-384",
			X: b64.EncodeToString(x), Y: b64.EncodeToString(y),
		}}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	issuer = server.URL

	v := NewVerifier(issuer, "mcp-harness")
	token := signES384(t, key, "ec1", map[string]any{
		"sub": "ec-user", "iss": issuer, "aud": "mcp-harness",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	claims, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("expected ES384 token to verify, got %v", err)
	}
	if claims.Subject != "ec-user" {
		t.Fatalf("unexpected subject: %q", claims.Subject)
	}

	// A signature from a different key must fail.
	other, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	bad := signES384(t, other, "ec1", map[string]any{
		"sub": "ec-user", "iss": issuer, "aud": "mcp-harness",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Verify(context.Background(), bad); err == nil {
		t.Fatal("expected mismatched-key signature to be rejected")
	}
}

func signES384(t *testing.T, key *ecdsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "ES384", "typ": "JWT", "kid": kid}
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	signingInput := b64.EncodeToString(h) + "." + b64.EncodeToString(c)
	digest := sha512.Sum384([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 96)
	r.FillBytes(sig[:48])
	s.FillBytes(sig[48:])
	return signingInput + "." + b64.EncodeToString(sig)
}

func TestPeekClaims(t *testing.T) {
	// Opaque token (Logto issues these when no resource is requested).
	if peek := PeekClaims("opaque-access-token"); peek.IsJWT {
		t.Fatal("opaque token must not be reported as a JWT")
	}
	// A JWT exposes its (unverified) claims for diagnostics.
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwt := signJWTUnsigned(t, key, map[string]any{
		"sub": "u1", "iss": "https://issuer/oidc", "aud": "api://mcp", "exp": time.Now().Add(time.Hour).Unix(),
	})
	peek := PeekClaims(jwt)
	if !peek.IsJWT || peek.Subject != "u1" || peek.Issuer != "https://issuer/oidc" || len(peek.Audience) != 1 || peek.Audience[0] != "api://mcp" {
		t.Fatalf("unexpected peek: %#v", peek)
	}
}

func signJWTUnsigned(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	return signJWT(t, key, "k", claims)
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
