package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type ManagedAssets struct {
	Agents string `json:"agents"`
	Skill  string `json:"skill"`
}

type ManagedMutation struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
	Detail   string `json:"detail"`
	Changed  bool   `json:"changed"`
}

type ManagedResult struct {
	Changed   bool
	Resources []string
	Mutations []ManagedMutation
}

type ManagedSnapshot struct {
	agents managedFileSnapshot
	skill  managedFileSnapshot
}

type ManagedSurfaceSet struct {
	AgentsPath string
	SkillPath  string
	write      func(string, []byte) error
}

type managedFileSnapshot struct {
	path   string
	data   []byte
	exists bool
}

type managedPlan struct {
	mutation ManagedMutation
	before   managedFileSnapshot
	after    []byte
}

func (s ManagedSurfaceSet) Preview(agentsEnabled bool, assets ManagedAssets) ([]ManagedMutation, error) {
	plans, err := s.plan(agentsEnabled, assets)
	if err != nil {
		return nil, err
	}
	mutations := make([]ManagedMutation, 0, len(plans))
	for _, plan := range plans {
		mutations = append(mutations, plan.mutation)
	}
	return mutations, nil
}

func (s ManagedSurfaceSet) Snapshot() (ManagedSnapshot, error) {
	if err := ValidateManagedFile(s.AgentsPath); err != nil {
		return ManagedSnapshot{}, err
	}
	if err := ValidateManagedFile(s.SkillPath); err != nil {
		return ManagedSnapshot{}, err
	}
	agents, err := snapshotManagedFile(s.AgentsPath)
	if err != nil {
		return ManagedSnapshot{}, err
	}
	skill, err := snapshotManagedFile(s.SkillPath)
	if err != nil {
		return ManagedSnapshot{}, err
	}
	return ManagedSnapshot{agents: agents, skill: skill}, nil
}

func (s ManagedSurfaceSet) Restore(snapshot ManagedSnapshot) error {
	return errors.Join(restoreManagedFile(snapshot.agents), restoreManagedFile(snapshot.skill))
}

func (s ManagedSurfaceSet) Reconcile(agentsEnabled bool, assets ManagedAssets) (ManagedResult, error) {
	plans, err := s.plan(agentsEnabled, assets)
	if err != nil {
		return ManagedResult{}, err
	}
	result := ManagedResult{Mutations: make([]ManagedMutation, 0, len(plans))}
	for _, plan := range plans {
		result.Mutations = append(result.Mutations, plan.mutation)
	}
	applied := make([]managedPlan, 0, len(plans))
	for _, plan := range plans {
		if !plan.mutation.Changed {
			continue
		}
		write := s.write
		if write == nil {
			write = writeManagedFile
		}
		if err := write(plan.before.path, plan.after); err != nil {
			attempted := append(applied, plan)
			return ManagedResult{}, errors.Join(err, rollbackManagedPlans(attempted))
		}
		applied = append(applied, plan)
		result.Changed = true
		result.Resources = append(result.Resources, plan.mutation.Resource)
	}
	if err := s.Verify(agentsEnabled, assets); err != nil {
		return ManagedResult{}, errors.Join(err, rollbackManagedPlans(applied))
	}
	return result, nil
}

func (s ManagedSurfaceSet) Verify(agentsEnabled bool, assets ManagedAssets) error {
	if err := VerifyManagedSurface(s.AgentsPath, agentsEnabled, []byte(assets.Agents)); err != nil {
		return fmt.Errorf("AGENTS.md: %w", err)
	}
	if err := VerifyManagedSurface(s.SkillPath, true, []byte(assets.Skill)); err != nil {
		return fmt.Errorf("skill: %w", err)
	}
	return nil
}

func (s ManagedSurfaceSet) plan(agentsEnabled bool, assets ManagedAssets) ([]managedPlan, error) {
	agents, err := planManagedFile("agents", s.AgentsPath, agentsEnabled, []byte(assets.Agents))
	if err != nil {
		return nil, fmt.Errorf("plan AGENTS.md: %w", err)
	}
	skill, err := planManagedFile("skill", s.SkillPath, true, []byte(assets.Skill))
	if err != nil {
		return nil, fmt.Errorf("plan skill: %w", err)
	}
	if agents.before.path == skill.before.path {
		return nil, fmt.Errorf("%w: AGENTS.md and skill resolve to the same file", ErrUnsafeManagedPath)
	}
	return []managedPlan{agents, skill}, nil
}

func planManagedFile(resource, path string, enabled bool, content []byte) (managedPlan, error) {
	resolved, err := resolveManagedPath(path)
	if err != nil {
		return managedPlan{}, err
	}
	if err := ValidateManagedFile(path); err != nil {
		return managedPlan{}, err
	}
	before, err := snapshotManagedFile(resolved)
	if err != nil {
		return managedPlan{}, err
	}
	var after []byte
	if enabled {
		after, err = UpdateManagedBlock(before.data, content)
	} else {
		after, err = RemoveManagedBlock(before.data)
	}
	if err != nil {
		return managedPlan{}, err
	}
	changed := !bytes.Equal(before.data, after)
	detail := path + ": no change"
	if changed && enabled {
		detail = path + ": write managed block"
	} else if changed {
		detail = path + ": remove managed block"
	}
	return managedPlan{mutation: ManagedMutation{Resource: resource, Path: path, Detail: detail, Changed: changed}, before: before, after: after}, nil
}

func snapshotManagedFile(path string) (managedFileSnapshot, error) {
	resolved, err := resolveManagedPath(path)
	if err != nil {
		return managedFileSnapshot{}, err
	}
	path = resolved
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return managedFileSnapshot{path: path}, nil
	}
	if err != nil {
		return managedFileSnapshot{}, err
	}
	return managedFileSnapshot{path: path, data: data, exists: true}, nil
}

func writeManagedFile(path string, data []byte) error {
	if len(data) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}
	return writeAtomic(path, data, 0o600)
}

func restoreManagedFile(snapshot managedFileSnapshot) error {
	if snapshot.path == "" {
		return nil
	}
	if !snapshot.exists {
		if err := rejectSymlinkComponents(snapshot.path); err != nil {
			return err
		}
		err := os.Remove(snapshot.path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(snapshot.path))
	}
	return writeAtomic(snapshot.path, snapshot.data, 0o600)
}

func rollbackManagedPlans(plans []managedPlan) error {
	var result error
	for index := len(plans) - 1; index >= 0; index-- {
		result = errors.Join(result, restoreManagedFile(plans[index].before))
	}
	return result
}
