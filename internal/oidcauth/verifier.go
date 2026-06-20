// Package oidcauth verifies OIDC bearer tokens (e.g. issued by Logto) for the
// MCP endpoint and discovers the identity provider's endpoints for the Web UI
// login flow. It uses only the standard library, so no JWT dependency is added.
package oidcauth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Discovery is the subset of the OIDC provider metadata we use.
type Discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint,omitempty"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
	Use string `json:"use"`
}

// Claims are the validated token claims callers care about.
type Claims struct {
	Subject  string
	Issuer   string
	Audience []string
	Email    string
	Expiry   time.Time
}

// Verifier validates RS256 JWTs against an issuer's JWKS.
type Verifier struct {
	issuer   string
	audience string
	client   *http.Client

	mu        sync.Mutex
	disc      *Discovery
	keys      map[string]*rsa.PublicKey
	keysAt    time.Time
	discAt    time.Time
	cacheFor  time.Duration
	discCache time.Duration
}

// NewVerifier builds a verifier for the given issuer and expected audience.
func NewVerifier(issuer, audience string) *Verifier {
	return &Verifier{
		issuer:    strings.TrimRight(issuer, "/"),
		audience:  audience,
		client:    &http.Client{Timeout: 10 * time.Second},
		cacheFor:  10 * time.Minute,
		discCache: time.Hour,
	}
}

// Discover fetches (and caches) the provider's OIDC metadata.
func (v *Verifier) Discover(ctx context.Context) (*Discovery, error) {
	v.mu.Lock()
	if v.disc != nil && time.Since(v.discAt) < v.discCache {
		d := v.disc
		v.mu.Unlock()
		return d, nil
	}
	v.mu.Unlock()

	url := v.issuer + "/.well-known/openid-configuration"
	var disc Discovery
	if err := v.getJSON(ctx, url, &disc); err != nil {
		return nil, fmt.Errorf("oidc discovery failed: %w", err)
	}
	if disc.JWKSURI == "" {
		return nil, errors.New("oidc discovery missing jwks_uri")
	}
	v.mu.Lock()
	v.disc = &disc
	v.discAt = time.Now()
	v.mu.Unlock()
	return &disc, nil
}

// Verify validates a raw JWT and returns its claims.
func (v *Verifier) Verify(ctx context.Context, token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("token is not a JWT")
	}
	headerBytes, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid token header")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, errors.New("invalid token header json")
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported token alg: %s", header.Alg)
	}
	key, err := v.keyForKid(ctx, header.Kid)
	if err != nil {
		return nil, err
	}
	signed := parts[0] + "." + parts[1]
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("invalid token signature encoding")
	}
	hashed := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hashed[:], sig); err != nil {
		return nil, errors.New("token signature verification failed")
	}

	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid token payload")
	}
	var raw struct {
		Sub   string          `json:"sub"`
		Iss   string          `json:"iss"`
		Aud   json.RawMessage `json:"aud"`
		Exp   int64           `json:"exp"`
		Email string          `json:"email"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, errors.New("invalid token claims json")
	}
	if raw.Sub == "" {
		return nil, errors.New("token missing sub")
	}
	if v.issuer != "" && strings.TrimRight(raw.Iss, "/") != v.issuer {
		return nil, fmt.Errorf("token issuer mismatch: %s", raw.Iss)
	}
	if raw.Exp != 0 && time.Now().After(time.Unix(raw.Exp, 0)) {
		return nil, errors.New("token expired")
	}
	aud := parseAudience(raw.Aud)
	if v.audience != "" && !contains(aud, v.audience) {
		return nil, errors.New("token audience mismatch")
	}
	return &Claims{Subject: raw.Sub, Issuer: raw.Iss, Audience: aud, Email: raw.Email, Expiry: time.Unix(raw.Exp, 0)}, nil
}

func (v *Verifier) keyForKid(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	if v.keys != nil && time.Since(v.keysAt) < v.cacheFor {
		if key, ok := v.keys[kid]; ok {
			v.mu.Unlock()
			return key, nil
		}
	}
	v.mu.Unlock()

	disc, err := v.Discover(ctx)
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := v.getJSON(ctx, disc.JWKSURI, &set); err != nil {
		return nil, fmt.Errorf("jwks fetch failed: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := rsaPublicKey(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	v.mu.Lock()
	v.keys = keys
	v.keysAt = time.Now()
	v.mu.Unlock()

	if key, ok := keys[kid]; ok {
		return key, nil
	}
	// kid may be empty when a provider publishes a single key.
	if kid == "" && len(keys) == 1 {
		for _, key := range keys {
			return key, nil
		}
	}
	return nil, fmt.Errorf("no JWKS key for kid %q", kid)
}

func (v *Verifier) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func rsaPublicKey(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := b64.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := b64.DecodeString(k.E)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	var e uint64
	switch len(eBytes) {
	case 0:
		return nil, errors.New("empty exponent")
	default:
		padded := make([]byte, 8)
		copy(padded[8-len(eBytes):], eBytes)
		e = binary.BigEndian.Uint64(padded)
	}
	return &rsa.PublicKey{N: n, E: int(e)}, nil
}

func parseAudience(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return []string{single}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

var b64 = base64.RawURLEncoding
