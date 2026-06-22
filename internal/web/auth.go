package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/TimLai666/mcp-harness/internal/oidcauth"
)

const sessionCookie = "mh_session"
const stateCookie = "mh_oauth_state"

// authConfig wires the Web UI to the OIDC provider (e.g. Logto). When OIDC is
// not configured, the console runs single-tenant as the default owner.
type authConfig struct {
	cfg            harness.OIDCConfig
	mcpToken       string
	tokenVerif     *oidcauth.Verifier // audience = MCP resource (for /mcp)
	idVerif        *oidcauth.Verifier // audience = client id (for Web UI id tokens)
	secret         []byte
	ghClientID     string
	ghClientSecret string
}

func newAuthConfig() *authConfig {
	cfg := harness.LoadOIDCConfig()
	ac := &authConfig{
		cfg:            cfg,
		mcpToken:       os.Getenv("MCP_HARNESS_MCP_BEARER_TOKEN"),
		secret:         sessionSecret(),
		ghClientID:     os.Getenv("MCP_HARNESS_GITHUB_CLIENT_ID"),
		ghClientSecret: os.Getenv("MCP_HARNESS_GITHUB_CLIENT_SECRET"),
	}
	if cfg.Enabled() {
		ac.tokenVerif = oidcauth.NewVerifier(cfg.Issuer, cfg.Audience)
		ac.idVerif = oidcauth.NewVerifier(cfg.Issuer, cfg.ClientID)
	}
	return ac
}

func (a *authConfig) githubEnabled() bool {
	return a.ghClientID != "" && a.ghClientSecret != ""
}

func (a *authConfig) enabled() bool { return a.cfg.Enabled() }

// owner resolves the tenant for a Web UI request. When OIDC is disabled it is
// always the default owner. When enabled, it reads the signed session cookie;
// the second return is false when the user is not logged in.
func (a *authConfig) owner(r *http.Request) (string, bool) {
	if !a.enabled() {
		return harness.DefaultOwner, true
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	uid := a.verifyCookie(cookie.Value)
	if uid == "" {
		return "", false
	}
	return harness.NormalizeOwner(uid), true
}

func (a *authConfig) resourceMetadataURL() string {
	if a.cfg.PublicURL == "" {
		return ""
	}
	return a.cfg.PublicURL + "/.well-known/oauth-protected-resource"
}

// registerAuthRoutes adds the OIDC login flow and the protected-resource
// metadata document used by MCP clients (ChatGPT) to discover the auth server.
func (a *authConfig) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled() {
			http.NotFound(w, r)
			return
		}
		resource := a.cfg.PublicURL + mcpEndpoint
		writeJSON(w, map[string]any{
			"resource":                 resource,
			"authorization_servers":    []string{a.cfg.Issuer},
			"scopes_supported":         []string{"openid", "profile", "email"},
			"bearer_methods_supported": []string{"header"},
		})
	})

	mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled() {
			http.Error(w, "oidc not configured", http.StatusNotFound)
			return
		}
		disc, err := a.tokenVerif.Discover(r.Context())
		if err != nil {
			log.Printf("[web-auth] login discovery failed (issuer=%s): %v", a.cfg.Issuer, err)
			http.Error(w, "oidc discovery failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		state := randomString()
		http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: state, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600, Secure: a.secureCookies()})
		q := url.Values{}
		q.Set("response_type", "code")
		q.Set("client_id", a.cfg.ClientID)
		q.Set("redirect_uri", a.cfg.PublicURL+"/auth/callback")
		q.Set("scope", "openid profile email")
		q.Set("state", state)
		if a.cfg.Audience != "" {
			q.Set("resource", a.cfg.Audience)
		}
		http.Redirect(w, r, disc.AuthorizationEndpoint+"?"+q.Encode(), http.StatusFound)
	})

	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled() {
			http.NotFound(w, r)
			return
		}
		stateC, err := r.Cookie(stateCookie)
		if err != nil || stateC.Value == "" || stateC.Value != r.URL.Query().Get("state") {
			http.Error(w, "invalid oauth state", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		uid, err := a.exchangeCode(r.Context(), code)
		if err != nil {
			log.Printf("[web-auth] callback token exchange failed: %v", err)
			http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		if authDebugWeb() {
			log.Printf("[web-auth] login ok: owner=%s", uid)
		}
		http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/", MaxAge: -1})
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: a.signCookie(uid), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 86400, Secure: a.secureCookies()})
		http.Redirect(w, r, "/", http.StatusFound)
	})

	// Logout is a browser navigation (GET): clear our cookie, then end the Logto
	// session too via RP-initiated logout, otherwise Logto would silently log the
	// user straight back in.
	logout := func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.secureCookies()})
		if a.enabled() {
			if disc, err := a.tokenVerif.Discover(r.Context()); err == nil && disc.EndSessionEndpoint != "" {
				q := url.Values{}
				q.Set("client_id", a.cfg.ClientID)
				q.Set("post_logout_redirect_uri", a.cfg.PublicURL+"/")
				http.Redirect(w, r, disc.EndSessionEndpoint+"?"+q.Encode(), http.StatusFound)
				return
			}
		}
		http.Redirect(w, r, "/", http.StatusFound)
	}
	mux.HandleFunc("GET /auth/logout", logout)
	mux.HandleFunc("POST /auth/logout", logout)
}

func (a *authConfig) exchangeCode(ctx context.Context, code string) (string, error) {
	disc, err := a.tokenVerif.Discover(ctx)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", a.cfg.PublicURL+"/auth/callback")
	// A public client (no secret) carries client_id in the body; a confidential
	// client authenticates via HTTP Basic below.
	if a.cfg.ClientSecret == "" {
		form.Set("client_id", a.cfg.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Authenticate the client with HTTP Basic (client_secret_basic), the OIDC
	// default. Per RFC 6749 §2.3.1 the id and secret are form-urlencoded before
	// base64.
	if a.cfg.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(a.cfg.ClientID), url.QueryEscape(a.cfg.ClientSecret))
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	if tok.IDToken == "" {
		return "", fmt.Errorf("no id_token in token response")
	}
	claims, err := a.idVerif.Verify(ctx, tok.IDToken)
	if err != nil {
		return "", fmt.Errorf("id_token verification failed: %w", err)
	}
	return claims.Subject, nil
}

func (a *authConfig) signCookie(uid string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(uid))
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

func (a *authConfig) verifyCookie(value string) string {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return ""
	}
	uid, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	return string(uid)
}

func (a *authConfig) secureCookies() bool {
	return strings.HasPrefix(a.cfg.PublicURL, "https://")
}

func authDebugWeb() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MCP_HARNESS_AUTH_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func sessionSecret() []byte {
	if raw := os.Getenv("MCP_HARNESS_SESSION_SECRET"); raw != "" {
		return []byte(raw)
	}
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return buf
}

func randomString() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}
