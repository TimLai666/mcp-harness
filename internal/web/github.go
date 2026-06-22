package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TimLai666/mcp-harness/internal/harness"
)

const githubStateCookie = "mh_gh_state"

// registerGitHubRoutes wires the per-user GitHub account connection. Each tenant
// (Logto user) links their own GitHub account; the token is stored in that
// tenant's isolated store, so private-repo access is scoped to the user.
func (a *authConfig) registerGitHubRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/github", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := a.owner(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]any{"error": "login required"})
			return
		}
		login, connected := harness.GitHubLogin(owner)
		writeJSON(w, map[string]any{"enabled": a.githubEnabled(), "connected": connected, "login": login})
	})

	mux.HandleFunc("GET /auth/github/login", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.owner(r); !ok {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		if !a.githubEnabled() {
			http.Error(w, "github oauth not configured", http.StatusNotFound)
			return
		}
		state := randomString()
		http.SetCookie(w, &http.Cookie{Name: githubStateCookie, Value: state, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600, Secure: a.secureCookies()})
		q := url.Values{}
		q.Set("client_id", a.ghClientID)
		q.Set("redirect_uri", a.cfg.PublicURL+"/auth/github/callback")
		q.Set("scope", "repo")
		q.Set("state", state)
		http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+q.Encode(), http.StatusFound)
	})

	mux.HandleFunc("GET /auth/github/callback", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := a.owner(r)
		if !ok {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		stateC, err := r.Cookie(githubStateCookie)
		if err != nil || stateC.Value == "" || stateC.Value != r.URL.Query().Get("state") {
			http.Error(w, "invalid github oauth state", http.StatusBadRequest)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: githubStateCookie, Value: "", Path: "/", MaxAge: -1})
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		token, login, err := a.githubExchange(r.Context(), code)
		if err != nil {
			log.Printf("[github-auth] connect failed for owner=%s: %v", owner, err)
			http.Error(w, "github connect failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		if err := harness.SetGitHubToken(owner, token, login); err != nil {
			http.Error(w, "could not store github token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	})

	disconnect := func(w http.ResponseWriter, r *http.Request) {
		owner, ok := a.owner(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]any{"error": "login required"})
			return
		}
		a.revokeGitHubGrant(r.Context(), owner)
		if err := harness.ClearGitHubToken(owner); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	}
	mux.HandleFunc("POST /auth/github/disconnect", disconnect)
}

// revokeGitHubGrant deletes the OAuth authorization grant on GitHub so the next
// Connect flow forces a fresh login + consent screen. Best-effort: a failure
// here just means GitHub will auto-approve next time (the local token is still
// cleared regardless).
func (a *authConfig) revokeGitHubGrant(ctx context.Context, owner string) {
	token, _, ok := harness.GetGitHubToken(owner)
	if !ok || token == "" {
		return
	}
	body := `{"access_token":"` + token + `"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		"https://api.github.com/applications/"+a.ghClientID+"/grant",
		strings.NewReader(body))
	if err != nil {
		log.Printf("[github] revoke grant request build failed: %v", err)
		return
	}
	req.SetBasicAuth(a.ghClientID, a.ghClientSecret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		log.Printf("[github] revoke grant request failed: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		log.Printf("[github] revoked grant for owner=%s", owner)
	} else {
		log.Printf("[github] revoke grant returned %d for owner=%s", resp.StatusCode, owner)
	}
}

// githubExchange swaps an authorization code for an access token and reads the
// linked account login.
func (a *authConfig) githubExchange(ctx context.Context, code string) (token, login string, err error) {
	form := url.Values{}
	form.Set("client_id", a.ghClientID)
	form.Set("client_secret", a.ghClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", a.cfg.PublicURL+"/auth/github/callback")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", "", err
	}
	if tok.AccessToken == "" {
		return "", "", fmt.Errorf("no access_token (%s: %s)", tok.Error, tok.ErrorDesc)
	}
	login, err = githubLogin(ctx, client, tok.AccessToken)
	if err != nil {
		return "", "", err
	}
	return tok.AccessToken, login, nil
}

func githubLogin(ctx context.Context, client *http.Client, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mcp-harness")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github /user returned %d", resp.StatusCode)
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", err
	}
	return user.Login, nil
}
