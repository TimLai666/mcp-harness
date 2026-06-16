package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SkillResources struct {
	Scripts    map[string]string `json:"scripts,omitempty"`
	References map[string]string `json:"references,omitempty"`
	Assets     map[string]string `json:"assets,omitempty"`
}

type SkillSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Path        string         `json:"path"`
	Resources   SkillResources `json:"resources"`
	content     string
}

type SkillRegistry struct {
	roots  []string
	skills map[string]*SkillSpec
}

func NewSkillRegistry() *SkillRegistry {
	roots := DefaultSkillRoots()
	reg := &SkillRegistry{roots: roots}
	reg.reload()
	return reg
}

func DefaultSkillRoots() []string {
	var roots []string
	if root, err := RepoRoot(); err == nil {
		roots = append(roots, filepath.Join(root, "skills"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots,
			filepath.Join(home, ".mcp-harness", "skills"),
			filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".claude", "skills"),
		)
	}
	return roots
}

func (r *SkillRegistry) List() []SkillSpec {
	r.reload()
	out := make([]SkillSpec, 0, len(r.skills))
	for _, skill := range r.skills {
		out = append(out, *skill)
	}
	return out
}

func (r *SkillRegistry) Get(name string) (*SkillSpec, error) {
	r.reload()
	key := NormalizeSkillName(name)
	skill, ok := r.skills[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	if skill.content == "" {
		data, err := os.ReadFile(filepath.Join(skill.Path, "SKILL.md"))
		if err != nil {
			return nil, err
		}
		_, body := SplitFrontmatter(string(data))
		skill.content = strings.TrimSpace(body)
	}
	return skill, nil
}

func (r *SkillRegistry) reload() {
	skills := map[string]*SkillSpec{}
	for _, root := range r.roots {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry == nil || entry.IsDir() || entry.Name() != "SKILL.md" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			meta, _ := SplitFrontmatter(string(data))
			name := strings.TrimSpace(meta["name"])
			if name == "" {
				name = filepath.Base(filepath.Dir(path))
			}
			desc := strings.TrimSpace(meta["description"])
			if desc == "" {
				return nil
			}
			key := NormalizeSkillName(name)
			if _, exists := skills[key]; exists {
				return nil
			}
			dir := filepath.Dir(path)
			skills[key] = &SkillSpec{
				Name:        name,
				Description: desc,
				Path:        dir,
				Resources:   LoadSkillResources(dir),
			}
			return nil
		})
	}
	r.skills = skills
}

func SplitFrontmatter(text string) (map[string]string, string) {
	if !strings.HasPrefix(text, "---") {
		return map[string]string{}, text
	}
	lines := strings.Split(text, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return map[string]string{}, text
	}
	return ParseFrontmatter(lines[1:end]), strings.Join(lines[end+1:], "\n")
}

func ParseFrontmatter(lines []string) map[string]string {
	out := map[string]string{}
	var key string
	var values []string
	flush := func() {
		if key != "" {
			out[key] = strings.TrimSpace(strings.Join(values, "\n"))
		}
	}
	for _, line := range lines {
		if strings.Contains(line, ":") && !strings.HasPrefix(line, " ") {
			flush()
			parts := strings.SplitN(line, ":", 2)
			key = strings.TrimSpace(parts[0])
			values = []string{strings.TrimSpace(parts[1])}
			continue
		}
		if key != "" {
			values = append(values, strings.TrimSpace(line))
		}
	}
	flush()
	return out
}

func NormalizeSkillName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return name
}

func LoadSkillResources(dir string) SkillResources {
	return SkillResources{
		Scripts:    listResourceDir(filepath.Join(dir, "scripts")),
		References: listResourceDir(filepath.Join(dir, "references")),
		Assets:     listResourceDir(filepath.Join(dir, "assets")),
	}
}

func listResourceDir(dir string) map[string]string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			out[entry.Name()] = filepath.Join(dir, entry.Name())
		}
	}
	return out
}
