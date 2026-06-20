package harness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const appDirEnv = "MCP_HARNESS_HOME"
const accessModeEnv = "MCP_HARNESS_ACCESS_MODE"
const accessModeSettingKey = "access_mode"

// DefaultOwner is the tenant id used when authentication is disabled, so the
// single-user/back-compat path behaves exactly like before multi-tenancy.
const DefaultOwner = "local"

// OIDCConfig holds the identity-provider settings (e.g. Logto) used to
// authenticate MCP and Web UI users. When Issuer is empty, auth is disabled and
// everything runs as DefaultOwner.
type OIDCConfig struct {
	Issuer       string
	Audience     string
	ClientID     string
	ClientSecret string
	PublicURL    string
}

// LoadOIDCConfig reads identity-provider settings from the environment.
func LoadOIDCConfig() OIDCConfig {
	return OIDCConfig{
		Issuer:       strings.TrimRight(os.Getenv("MCP_HARNESS_OIDC_ISSUER"), "/"),
		Audience:     os.Getenv("MCP_HARNESS_OIDC_AUDIENCE"),
		ClientID:     os.Getenv("MCP_HARNESS_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("MCP_HARNESS_OIDC_CLIENT_SECRET"),
		PublicURL:    strings.TrimRight(os.Getenv("MCP_HARNESS_PUBLIC_URL"), "/"),
	}
}

// Enabled reports whether authentication/multi-tenancy is configured.
func (c OIDCConfig) Enabled() bool { return c.Issuer != "" }

// NormalizeOwner returns a safe, non-empty tenant id.
func NormalizeOwner(owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return DefaultOwner
	}
	return owner
}

// OwnerDirName maps an owner id to a filesystem-safe directory name for
// per-user workspaces and sandboxes.
func OwnerDirName(owner string) string {
	owner = NormalizeOwner(owner)
	var b strings.Builder
	for _, r := range owner {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" {
		name = DefaultOwner
	}
	return name
}

type projectsFile struct {
	Projects []Project `json:"projects"`
}

// CurrentAccessMode resolves the server-side access policy. The agent cannot
// set this; it is controlled by the operator through the Web UI (persisted in
// the settings table) with an environment-variable fallback. When nothing is
// configured the policy is `default`, which routes high-risk operations into
// the approval queue.
func CurrentAccessMode() AccessMode { return CurrentAccessModeFor(DefaultOwner) }

// CurrentAccessModeFor resolves the access policy for one tenant.
func CurrentAccessModeFor(owner string) AccessMode {
	if store, err := DefaultStoreFor(owner); err == nil {
		if value, ok, err := store.GetSetting(accessModeSettingKey); err == nil && ok {
			if mode := normalizeAccessMode(value); mode != "" {
				return mode
			}
		}
	}
	if mode := normalizeAccessMode(os.Getenv(accessModeEnv)); mode != "" {
		return mode
	}
	return AccessDefault
}

// SetAccessMode persists the operator's access policy. Only `default` and
// `full_access` are accepted; the agent never calls this.
func SetAccessMode(mode AccessMode) error { return SetAccessModeFor(DefaultOwner, mode) }

func SetAccessModeFor(owner string, mode AccessMode) error {
	normalized := normalizeAccessMode(string(mode))
	if normalized == "" {
		return errors.New("invalid access mode: must be default or full_access")
	}
	store, err := DefaultStoreFor(owner)
	if err != nil {
		return err
	}
	return store.SetSetting(accessModeSettingKey, string(normalized))
}

func normalizeAccessMode(value string) AccessMode {
	switch AccessMode(value) {
	case AccessDefault:
		return AccessDefault
	case AccessFullAccess:
		return AccessFullAccess
	default:
		return ""
	}
}

func AppDir() (string, error) {
	if raw := os.Getenv(appDirEnv); raw != "" {
		return ensureDir(raw)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(home, ".mcp-harness"))
}

// TenantHome is the per-owner storage root. The default owner uses the app dir
// directly (back-compat); every other tenant gets an isolated subtree under
// tenants/<owner>, so their DB, workspaces, sandbox, and history never mix.
func TenantHome(owner string) (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	if NormalizeOwner(owner) == DefaultOwner {
		return base, nil
	}
	return ensureDir(filepath.Join(base, "tenants", OwnerDirName(owner)))
}

func SandboxDir() (string, error) { return SandboxDirFor(DefaultOwner) }

// SandboxDirFor returns the per-owner sandbox.
func SandboxDirFor(owner string) (string, error) {
	home, err := TenantHome(owner)
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(home, "sandbox"))
}

func WorkspacesDir() (string, error) { return WorkspacesDirFor(DefaultOwner) }

// WorkspacesDirFor returns the per-owner harness-managed workspaces root.
func WorkspacesDirFor(owner string) (string, error) {
	home, err := TenantHome(owner)
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(home, "workspaces"))
}

func SessionsDir() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "sessions"))
}

func ProjectsPath() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "projects.json"), nil
}

func DBPath() (string, error) { return DBPathFor(DefaultOwner) }

// DBPathFor returns the per-owner SQLite database path.
func DBPathFor(owner string) (string, error) {
	home, err := TenantHome(owner)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "harness.db"), nil
}

func MCPsPath() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mcps.json"), nil
}

func ApprovalsDir() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "approvals"))
}

func HistoryDir() (string, error) {
	base, err := AppDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "history"))
}

func HistoryVersionsDir() (string, error) {
	base, err := HistoryDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "versions"))
}

func HistoryBlobsDir() (string, error) {
	base, err := HistoryDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "blobs"))
}

func RepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "prompts", "main.md")); err == nil {
			return wd, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			return "", errors.New("could not locate repository root")
		}
		wd = next
	}
}

func LoadProjects() ([]Project, error) { return LoadProjectsFor(DefaultOwner) }

func LoadProjectsFor(owner string) ([]Project, error) {
	store, err := DefaultStoreFor(owner)
	if err != nil {
		return nil, err
	}
	projects, err := store.ListProjects()
	if err != nil {
		return nil, err
	}
	for i := range projects {
		abs, err := filepath.Abs(projects[i].Path)
		if err == nil {
			projects[i].Path = abs
		}
		if projects[i].DefaultMode == "" {
			projects[i].DefaultMode = ModeInspect
		}
		projects[i].Owner = NormalizeOwner(owner)
	}
	return projects, nil
}

func SaveProjects(projects []Project) error { return SaveProjectsFor(DefaultOwner, projects) }

func SaveProjectsFor(owner string, projects []Project) error {
	store, err := DefaultStoreFor(owner)
	if err != nil {
		return err
	}
	return store.SaveProjects(projects)
}

func ReadPrompt(name string) string {
	root, err := RepoRoot()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, "prompts", name))
	if err != nil {
		return ""
	}
	return string(data)
}

func ensureDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return "", err
	}
	return abs, nil
}
