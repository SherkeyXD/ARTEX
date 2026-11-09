package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillFrontmatter(t *testing.T) {
	doc := `---
name: my-skill
description: A helpful testing skill
mcps:
  - server-a
  - server-b
---

# My Skill Instructions
Step 1: Do recon
Step 2: Exploit
`
	s := ParseSkill([]byte(doc))
	if s.Name != "my-skill" {
		t.Fatalf("expected name my-skill, got %q", s.Name)
	}
	if s.Description != "A helpful testing skill" {
		t.Fatalf("expected description, got %q", s.Description)
	}
	if len(s.MCPs) != 2 || s.MCPs[0] != "server-a" || s.MCPs[1] != "server-b" {
		t.Fatalf("unexpected mcps: %v", s.MCPs)
	}
	if !strings.Contains(s.Instructions, "# My Skill Instructions") {
		t.Fatalf("unexpected instructions: %q", s.Instructions)
	}
}

func TestLoadDir(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := `---
name: subfolder-skill
description: Loaded from directory
---
Instructions in subfolder
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadDir(tmpDir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}
	s, ok := reg.Get("subfolder-skill")
	if !ok {
		t.Fatalf("expected to find subfolder-skill")
	}
	if s.Dir != skillDir {
		t.Fatalf("expected Dir %q, got %q", skillDir, s.Dir)
	}

	// Test UniversalTool
	tool := reg.UniversalTool()
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"name": "subfolder-skill"}`))
	if err != nil {
		t.Fatalf("UniversalTool Execute failed: %v", err)
	}
	str, ok := res.(string)
	if !ok || !strings.Contains(str, "Instructions in subfolder") {
		t.Fatalf("unexpected tool output: %v", res)
	}
}
