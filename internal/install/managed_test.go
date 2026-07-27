package install

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedSurfaceSetRefreshesOldContentAndPreservesOutsideBytes(t *testing.T) {
	directory := t.TempDir()
	agents := filepath.Join(directory, "AGENTS.md")
	skill := filepath.Join(directory, "skills", "threadbear", "SKILL.md")
	old := []byte("before\r\n" + ManagedBlockStart + "\nold\n" + ManagedBlockEnd + "\nafter\r\n")
	if err := os.WriteFile(agents, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(skill), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, old, 0o600); err != nil {
		t.Fatal(err)
	}
	set := ManagedSurfaceSet{AgentsPath: agents, SkillPath: skill}
	result, err := set.Reconcile(true, ManagedAssets{Agents: "new agents", Skill: "new skill"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || len(result.Resources) != 2 {
		t.Fatalf("result=%+v", result)
	}
	agentsData, _ := os.ReadFile(agents)
	if !bytes.HasPrefix(agentsData, []byte("before\r\n")) || !bytes.HasSuffix(agentsData, []byte("after\r\n")) || bytes.Count(agentsData, []byte(ManagedBlockStart)) != 1 {
		t.Fatalf("agents=%q", agentsData)
	}
	clean, err := set.Reconcile(true, ManagedAssets{Agents: "new agents", Skill: "new skill"})
	if err != nil || clean.Changed || len(clean.Resources) != 0 {
		t.Fatalf("clean=%+v err=%v", clean, err)
	}
}

func TestManagedSurfaceSetAgentsDisabledDoesNotRestoreAgents(t *testing.T) {
	directory := t.TempDir()
	agents := filepath.Join(directory, "AGENTS.md")
	skill := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(agents, []byte("user only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	set := ManagedSurfaceSet{AgentsPath: agents, SkillPath: skill}
	result, err := set.Reconcile(false, ManagedAssets{Agents: "managed agents", Skill: "managed skill"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(agents)
	if string(data) != "user only\n" {
		t.Fatalf("agents=%q", data)
	}
	if len(result.Resources) != 1 || result.Resources[0] != "skill" {
		t.Fatalf("resources=%v", result.Resources)
	}
}

func TestManagedSurfaceSetNoOpPerformsNoWrites(t *testing.T) {
	directory := t.TempDir()
	set := ManagedSurfaceSet{AgentsPath: filepath.Join(directory, "AGENTS.md"), SkillPath: filepath.Join(directory, "SKILL.md")}
	assets := ManagedAssets{Agents: "agents", Skill: "skill"}
	if _, err := set.Reconcile(true, assets); err != nil {
		t.Fatal(err)
	}
	writes := 0
	set.write = func(string, []byte) error {
		writes++
		return nil
	}
	result, err := set.Reconcile(true, assets)
	if err != nil || result.Changed || writes != 0 {
		t.Fatalf("result=%+v writes=%d err=%v", result, writes, err)
	}
}

func TestManagedSurfaceSetRejectsMalformedBeforeWrites(t *testing.T) {
	directory := t.TempDir()
	agents := filepath.Join(directory, "AGENTS.md")
	skill := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(agents, append(ManagedBlock([]byte("one")), ManagedBlock([]byte("two"))...), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	set := ManagedSurfaceSet{AgentsPath: agents, SkillPath: skill, write: func(string, []byte) error { writes++; return nil }}
	if _, err := set.Reconcile(true, ManagedAssets{Agents: "new", Skill: "new"}); err == nil {
		t.Fatal("reconcile succeeded")
	}
	if writes != 0 {
		t.Fatalf("writes=%d", writes)
	}
}

func TestManagedSurfaceSetFollowsOwnedSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	agents := filepath.Join(directory, "AGENTS.md")
	skill := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(target, []byte("user content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, agents); err != nil {
		t.Fatal(err)
	}
	set := ManagedSurfaceSet{AgentsPath: agents, SkillPath: skill}
	if _, err := set.Reconcile(true, ManagedAssets{Agents: "new", Skill: "skill"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("user content")) || !bytes.Contains(data, []byte("new")) {
		t.Fatalf("target=%q", data)
	}
	info, err := os.Lstat(agents)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("managed symlink was replaced")
	}
}

func TestManagedSurfaceSetRollsBackFirstSurfaceWhenSecondWriteFails(t *testing.T) {
	directory := t.TempDir()
	agents := filepath.Join(directory, "AGENTS.md")
	skill := filepath.Join(directory, "SKILL.md")
	originalAgents := []byte("user\n")
	originalSkill := ManagedBlock([]byte("old skill"))
	if err := os.WriteFile(agents, originalAgents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, originalSkill, 0o600); err != nil {
		t.Fatal(err)
	}
	resolvedSkill, err := resolveManagedPath(skill)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	set := ManagedSurfaceSet{AgentsPath: agents, SkillPath: skill}
	set.write = func(path string, data []byte) error {
		writes++
		if path == resolvedSkill {
			return errors.New("skill write failed")
		}
		return writeManagedFile(path, data)
	}
	if _, err := set.Reconcile(true, ManagedAssets{Agents: "new agents", Skill: "new skill"}); err == nil {
		t.Fatal("reconcile succeeded")
	}
	data, _ := os.ReadFile(agents)
	if !bytes.Equal(data, originalAgents) || writes != 2 {
		t.Fatalf("agents=%q writes=%d", data, writes)
	}
}

func TestManagedSurfaceSetRollbackRemovesPreviouslyAbsentSurface(t *testing.T) {
	directory := t.TempDir()
	agents := filepath.Join(directory, "AGENTS.md")
	skill := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(skill, ManagedBlock([]byte("old skill")), 0o600); err != nil {
		t.Fatal(err)
	}
	resolvedSkill, err := resolveManagedPath(skill)
	if err != nil {
		t.Fatal(err)
	}
	set := ManagedSurfaceSet{AgentsPath: agents, SkillPath: skill}
	set.write = func(path string, data []byte) error {
		if path == resolvedSkill {
			return errors.New("skill write failed")
		}
		return writeManagedFile(path, data)
	}
	if _, err := set.Reconcile(true, ManagedAssets{Agents: "new agents", Skill: "new skill"}); err == nil {
		t.Fatal("reconcile succeeded")
	}
	if _, err := os.Stat(agents); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("previously absent surface was not removed: %v", err)
	}
}

func TestManagedSurfaceSetRollsBackSurfaceWhenWriterCommitsThenErrors(t *testing.T) {
	directory := t.TempDir()
	agents := filepath.Join(directory, "AGENTS.md")
	skill := filepath.Join(directory, "SKILL.md")
	originalAgents := []byte("user agents\n")
	originalSkill := []byte("user skill\n")
	if err := os.WriteFile(agents, originalAgents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, originalSkill, 0o600); err != nil {
		t.Fatal(err)
	}
	set := ManagedSurfaceSet{AgentsPath: agents, SkillPath: skill}
	set.write = func(path string, data []byte) error {
		if err := writeManagedFile(path, data); err != nil {
			return err
		}
		return errors.New("directory sync failed after commit")
	}
	_, err := set.Reconcile(true, ManagedAssets{Agents: "new agents", Skill: "new skill"})
	if err == nil || !strings.Contains(err.Error(), "directory sync failed after commit") {
		t.Fatalf("err=%v", err)
	}
	agentsData, _ := os.ReadFile(agents)
	skillData, _ := os.ReadFile(skill)
	if !bytes.Equal(agentsData, originalAgents) || !bytes.Equal(skillData, originalSkill) {
		t.Fatalf("agents=%q skill=%q", agentsData, skillData)
	}
}
