package harness

import (
	"os/exec"
	"strings"
)

// DetectRuntimes probes the system PATH for expected language runtimes and
// package managers, returning those that are available and their versions.
func DetectRuntimes() []RuntimeInfo {
	type probe struct {
		name     string
		command  string
		args     []string
		parseVer func(string) string
	}
	probes := []probe{
		{name: "uv", command: "uv", args: []string{"--version"}, parseVer: firstToken},
		{name: "nodejs", command: "node", args: []string{"--version"}, parseVer: firstToken},
		{name: "bun", command: "bun", args: []string{"--version"}, parseVer: firstToken},
		{name: "go", command: "go", args: []string{"version"}, parseVer: goVersion},
	}

	out := make([]RuntimeInfo, 0, len(probes))
	for _, p := range probes {
		if _, err := exec.LookPath(p.command); err != nil {
			continue
		}
		cmd := exec.Command(p.command, p.args...)
		b, err := cmd.Output()
		if err != nil {
			out = append(out, RuntimeInfo{Name: p.name, Version: ""})
			continue
		}
		ver := strings.TrimSpace(string(b))
		if p.parseVer != nil {
			ver = p.parseVer(ver)
		}
		out = append(out, RuntimeInfo{Name: p.name, Version: ver})
	}
	return out
}

func firstToken(s string) string {
	parts := strings.Fields(s)
	if len(parts) > 0 {
		return parts[0]
	}
	return s
}

func goVersion(s string) string {
	// "go version go1.23.0 darwin/amd64" -> "go1.23.0"
	parts := strings.Fields(s)
	for _, p := range parts {
		if strings.HasPrefix(p, "go") {
			return p
		}
	}
	if len(parts) >= 3 {
		return parts[2]
	}
	return s
}
