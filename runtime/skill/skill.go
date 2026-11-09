// Package skill implements the skill system: named, reusable instruction packages
// the agent can invoke on demand. The format follows the agentskills.io open
// standard — a SKILL.md with YAML frontmatter followed by Markdown instructions.
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
	actool "github.com/SherkeyXD/ARTEX/runtime/tool"
	"gopkg.in/yaml.v3"
)

// Skill represents one invokable instruction package.
type Skill struct {
	Name          string   `yaml:"name" json:"name"`
	Description   string   `yaml:"description" json:"description"`
	License       string   `yaml:"license,omitempty" json:"license,omitempty"`
	Compatibility string   `yaml:"compatibility,omitempty" json:"compatibility,omitempty"`
	WhenToUse     string   `yaml:"when_to_use,omitempty" json:"when_to_use,omitempty"`
	Instructions  string   `yaml:"-" json:"instructions"`
	MCPs          []string `yaml:"mcps,omitempty" json:"mcps,omitempty"`
	Dir           string   `yaml:"-" json:"dir,omitempty"`
}

// Registry is a thread-safe name-indexed set of skills.
type Registry struct {
	mu       sync.RWMutex
	byName   map[string]Skill
	OnInvoke func(Skill) string
}

// NewRegistry creates a new registry containing the specified skills.
func NewRegistry(skills ...Skill) *Registry {
	r := &Registry{byName: make(map[string]Skill)}
	for _, s := range skills {
		r.Add(s)
	}
	return r
}

// Add inserts or updates a skill in the registry.
func (r *Registry) Add(s Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byName == nil {
		r.byName = make(map[string]Skill)
	}
	r.byName[strings.ToLower(s.Name)] = s
}

// Get looks up a skill by name (case-insensitive).
func (r *Registry) Get(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byName[strings.ToLower(name)]
	return s, ok
}

// List returns all skills in the registry sorted by name.
func (r *Registry) List() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Skill, 0, len(r.byName))
	for _, s := range r.byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (r *Registry) names() []string {
	skills := r.List()
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = s.Name
	}
	return out
}

func (r *Registry) listing() string {
	skills := r.List()
	var sb strings.Builder
	for _, s := range skills {
		sb.WriteString(fmt.Sprintf("\n- %s: %s", s.Name, s.Description))
	}
	return sb.String()
}

// ParseSkill parses skill markdown data with optional YAML frontmatter.
func ParseSkill(data []byte) Skill {
	content := string(data)
	trimmed := strings.TrimLeft(content, "\r\n ")
	if strings.HasPrefix(trimmed, "---") {
		// Drop opening ---
		rest := strings.TrimPrefix(trimmed, "---")
		rest = strings.TrimPrefix(rest, "\r")
		rest = strings.TrimPrefix(rest, "\n")

		// Find closing ---
		idx := strings.Index(rest, "\n---")
		if idx != -1 {
			fm := rest[:idx]
			body := rest[idx+4:]
			body = strings.TrimPrefix(body, "\r")
			body = strings.TrimPrefix(body, "\n")

			var s Skill
			if err := yaml.Unmarshal([]byte(fm), &s); err == nil {
				s.Instructions = body
				return s
			}
		}
	}
	return Skill{Instructions: content}
}

// LoadDir loads skills from subdirectories in dir or markdown files.
func LoadDir(dir string) (*Registry, error) {
	r := NewRegistry()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		var path string
		switch {
		case e.IsDir():
			p := filepath.Join(dir, e.Name(), "SKILL.md")
			if _, statErr := os.Stat(p); statErr == nil {
				path = p
			}
		case strings.HasSuffix(e.Name(), ".md"):
			path = filepath.Join(dir, e.Name())
		}
		if path == "" {
			continue
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			continue
		}
		s := ParseSkill(data)
		if s.Name == "" {
			s.Name = strings.TrimSuffix(e.Name(), ".md")
		}
		if e.IsDir() {
			s.Dir = filepath.Join(dir, e.Name())
		} else {
			s.Dir = dir
		}
		r.Add(s)
	}
	return r, nil
}

// Tool builds a legacy actool.CoreTool to invoke skills.
func (r *Registry) Tool() actool.CoreTool {
	avail := r.listing()
	return actool.Build(actool.Spec{
		Name:        "Skill",
		Description: "Invokes a named skill — a reusable procedure — and returns its step-by-step instructions for you to follow. Use a skill when the task matches one of the available skills below.",
		Prompt:      "Available skills:" + avail,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "The skill to invoke."},
				"args": map[string]any{"type": "string", "description": "Optional additional context for the skill."},
			},
			"required": []any{"name"},
		},
		ReadOnly:   func(json.RawMessage) bool { return true },
		Concurrent: func(json.RawMessage) bool { return false },
		Run: func(_ context.Context, input json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var in struct {
				Name string `json:"name"`
				Args string `json:"args"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return actool.Result{}, err
			}
			s, ok := r.Get(in.Name)
			if !ok {
				return actool.Errorf(fmt.Sprintf("Error: unknown skill %q. Available: %s", in.Name, strings.Join(r.names(), ", "))), nil
			}
			instructions := s.Instructions
			if s.Dir != "" {
				instructions = strings.ReplaceAll(instructions, "${SKILL_DIR}", s.Dir)
			}
			body := instructions
			if strings.TrimSpace(in.Args) != "" {
				body += "\n\n## Additional context from the caller\n\n" + in.Args
			}
			if r.OnInvoke != nil {
				if extra := r.OnInvoke(s); extra != "" {
					body += "\n\n" + extra
				}
			}
			result := actool.Text(fmt.Sprintf("Skill %q loaded. Follow its instructions in the next message.", s.Name))
			result.Extra = []llm.Message{llm.UserText(body)}
			return result, nil
		},
	})
}

// UniversalTool uses the same result contract as Tool, including full skill
// instructions as a separate message and ordinary errors for unknown skills.
func (r *Registry) UniversalTool() runtime.Tool { return r.Tool() }
