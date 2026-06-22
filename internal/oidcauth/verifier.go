// Package oidcauth verifies OIDC bearer tokens (e.g. issued by Logto) for the
// MCP endpoint and discovers the identity provider's endpoints for the Web UI
// login flow. It uses only the standard library, so no JWT dependency is added.
package oidcauth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
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
	Alg string `json:"alg"`
	Use string `json:"use"`
	// RSA
	N string `json:"n"`
	E string `json:"e"`
	// EC
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
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
	keys      map[string]crypto.PublicKey
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
	hash, ok := algHash(header.Alg)
	if !ok {
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
	h := hash.New()
	h.Write([]byte(signed))
	digest := h.Sum(nil)
	if err := verifySignature(header.Alg, key, digest, sig); err != nil {
		return nil, err
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

// Issuer and Audience expose the verifier's expected values for diagnostics.
func (v *Verifier) Issuer() string   { return v.issuer }
func (v *Verifier) Audience() string { return v.audience }

// PeekedToken is an UNVERIFIED view of a token, for logging only. Never trust
// these values for authorization.
type PeekedToken struct {
	IsJWT    bool
	Alg      string
	Kid      string
	Issuer   string
	Subject  string
	Audience []string
	Expiry   time.Time
}

// PeekClaims decodes a token's header and payload without verifying the
// signature. It exists purely to produce helpful auth-failure diagnostics (for
// example, distinguishing an opaque token from a JWT, or an audience mismatch).
func PeekClaims(token string) PeekedToken {
	peek := PeekedToken{}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return peek // opaque / not a JWT
	}
	peek.IsJWT = true
	if hb, err := b64.DecodeString(parts[0]); err == nil {
		var h struct {
			Alg string `json:"alg"`
			Kid string `json:"kid"`
		}
		if json.Unmarshal(hb, &h) == nil {
			peek.Alg = h.Alg
			peek.Kid = h.Kid
		}
	}
	if pb, err := b64.DecodeString(parts[1]); err == nil {
		var c struct {
			Sub string          `json:"sub"`
			Iss string          `json:"iss"`
			Aud json.RawMessage `json:"aud"`
			Exp int64           `json:"exp"`
		}
		if json.Unmarshal(pb, &c) == nil {
			peek.Subject = c.Sub
			peek.Issuer = c.Iss
			peek.Audience = parseAudience(c.Aud)
			if c.Exp != 0 {
				peek.Expiry = time.Unix(c.Exp, 0)
			}
		}
	}
	return peek
}

func (v *Verifier) keyForKid(ctx context.Context, kid string) (crypto.PublicKey, error) {
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
	keys := map[string]crypto.PublicKey{}
	for _, k := range set.Keys {
		pub, err := publicKeyFromJWK(k)
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

func publicKeyFromJWK(k jwk) (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		return rsaPublicKey(k)
	case "EC":
		return ecPublicKey(k)
	default:
		return nil, fmt.Errorf("unsupported key type: %s", k.Kty)
	}
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
	if len(eBytes) == 0 {
		return nil, errors.New("empty exponent")
	}
	n := new(big.Int).SetBytes(nBytes)
	padded := make([]byte, 8)
	copy(padded[8-len(eBytes):], eBytes)
	e := binary.BigEndian.Uint64(padded)
	return &rsa.PublicKey{N: n, E: int(e)}, nil
}

func ecPublicKey(k jwk) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported EC curve: %s", k.Crv)
	}
	xBytes, err := b64.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	yBytes, err := b64.DecodeString(k.Y)
	if err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(xBytes), Y: new(big.Int).SetBytes(yBytes)}, nil
}

func algHash(alg string) (crypto.Hash, bool) {
	switch alg {
	case "RS256", "ES256":
		return crypto.SHA256, true
	case "ES384":
		return crypto.SHA384, true
	case "ES512":
		return crypto.SHA512, true
	default:
		return 0, false
	}
}

func verifySignature(alg string, key crypto.PublicKey, digest, sig []byte) error {
	switch alg {
	case "RS256":
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return errors.New("token alg/key mismatch: expected RSA key")
		}
		hash, _ := algHash(alg)
		if err := rsa.VerifyPKCS1v15(rsaKey, hash, digest, sig); err != nil {
			return errors.New("token signature verification failed")
		}
		return nil
	case "ES256", "ES384", "ES512":
		ecKey, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("token alg/key mismatch: expected EC key")
		}
		// JWS ECDSA signatures are the raw concatenation r||s, each padded to the
		// curve's byte size (not ASN.1 DER).
		size := (ecKey.Curve.Params().BitSize + 7) / 8
		if len(sig) != 2*size {
			return fmt.Errorf("invalid ECDSA signature length: got %d, want %d", len(sig), 2*size)
		}
		r := new(big.Int).SetBytes(sig[:size])
		s := new(big.Int).SetBytes(sig[size:])
		if !ecdsa.Verify(ecKey, digest, r, s) {
			return errors.New("token signature verification failed")
		}
		return nil
	default:
		return fmt.Errorf("unsupported token alg: %s", alg)
	}
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
