package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/ericlitman/threadbear/internal/state"
)

const (
	uninstallLockWait          = 30 * time.Second
	uninstallLockRetryInterval = 100 * time.Millisecond
)

var ErrHeartbeatInFlight = errors.New("heartbeat in flight")

type ChoicePrompter interface {
	ShowMessage(string) error
	ShowPreview(Preview) error
	Confirm(defaultYes bool) (bool, error)
	Choose(string, bool) (bool, error)
}

type UninstallRequest struct {
	NonInteractive     bool
	Confirm            bool
	ArchiveControlTask bool
}

type UninstallResult struct {
	ArchivedControlTask bool
	DeletedState        bool
	Preview             Preview
	Changed             bool
	Resources           []string
}

type Uninstaller struct {
	Paths        Paths
	Store        Store
	Scheduler    Scheduler
	ControlTasks ControlTasks
	Prompter     ChoicePrompter
	Previewer    func(Preview) error
}

func (u Uninstaller) Uninstall(ctx context.Context, request UninstallRequest) (UninstallResult, error) {
	if u.Store == nil || u.Scheduler == nil || u.ControlTasks == nil {
		return UninstallResult{}, errors.New("uninstaller dependencies are required")
	}
	archiveControlTask := request.ArchiveControlTask
	if request.NonInteractive {
		if !request.Confirm {
			return UninstallResult{}, errors.New("noninteractive uninstall requires confirm")
		}
	} else {
		if u.Prompter == nil {
			return UninstallResult{}, errors.New("interactive uninstall requires a prompter")
		}
		if err := u.Prompter.ShowMessage("Thanks for using ThreadBear. I'd love any feedback on why this wasn't for you. Drop me an email at eric@litman.org if you're open to sharing. Now, on to the uninstall!"); err != nil {
			return UninstallResult{}, err
		}
		var err error
		archiveControlTask, err = u.Prompter.Choose("Archive the ThreadBear control task", true)
		if err != nil {
			return UninstallResult{}, err
		}
	}
	agentsPreview, _, err := ManagedMutationPreview(u.Paths.Agents, false, nil)
	if err != nil {
		return UninstallResult{}, fmt.Errorf("preview AGENTS.md mutation: %w", err)
	}
	skillPreview, _, err := ManagedMutationPreview(u.Paths.Skill, false, nil)
	if err != nil {
		return UninstallResult{}, fmt.Errorf("preview skill mutation: %w", err)
	}
	preview := Preview{Operation: "uninstall", Lines: []string{
		"remove LaunchAgent: " + u.Paths.LaunchAgent,
		"remove binary: " + u.Paths.Binary,
		agentsPreview,
		skillPreview,
		fmt.Sprintf("archive control task: %t", archiveControlTask),
		fmt.Sprintf("persistent state %s: delete=true", u.Paths.StateDirectory),
	}}
	if u.Prompter != nil {
		if err := u.Prompter.ShowPreview(preview); err != nil {
			return UninstallResult{}, err
		}
	} else if u.Previewer != nil {
		if err := u.Previewer(preview); err != nil {
			return UninstallResult{}, err
		}
	}
	if !request.NonInteractive {
		confirmed, err := u.Prompter.Confirm(true)
		if err != nil {
			return UninstallResult{}, err
		}
		if !confirmed {
			return UninstallResult{}, ErrCancelled
		}
	}
	resources := make([]string, 0, 6)
	existed := func(path string) bool { _, err := os.Lstat(path); return err == nil }
	hadLaunchAgent := existed(u.Paths.LaunchAgent)
	hadBinary := existed(u.Paths.Binary)
	hadAgents := managedFileHasBlock(u.Paths.Agents)
	hadSkill := managedFileHasBlock(u.Paths.Skill)
	hadState := existed(u.Paths.StateDirectory)
	schedulerLoaded, err := u.Scheduler.Loaded(ctx)
	if err != nil {
		return UninstallResult{}, fmt.Errorf("inspect scheduler: %w", err)
	}
	if !hadLaunchAgent && !schedulerLoaded && !hadBinary && !hadAgents && !hadSkill && !hadState {
		return UninstallResult{Preview: preview, Changed: false, Resources: []string{}}, nil
	}
	lock, err := acquireUninstallLock(ctx, u.Store, uninstallLockWait, uninstallLockRetryInterval)
	if err != nil {
		return UninstallResult{}, err
	}
	lockHeld := true
	defer func() {
		if lockHeld {
			_ = lock.Close()
		}
	}()
	hadLaunchAgent = existed(u.Paths.LaunchAgent)
	hadBinary = existed(u.Paths.Binary)
	hadAgents = managedFileHasBlock(u.Paths.Agents)
	hadSkill = managedFileHasBlock(u.Paths.Skill)
	hadState = existed(u.Paths.StateDirectory)
	schedulerLoaded, err = u.Scheduler.Loaded(ctx)
	if err != nil {
		return UninstallResult{}, fmt.Errorf("recheck scheduler under lock: %w", err)
	}
	if !hadLaunchAgent && !schedulerLoaded && !hadBinary && !hadAgents && !hadSkill && !hadState {
		if err := lock.Close(); err != nil {
			return UninstallResult{}, fmt.Errorf("release uninstall lock: %w", err)
		}
		lockHeld = false
		return UninstallResult{Preview: preview, Changed: false, Resources: []string{}}, nil
	}
	var controlTaskID string
	if archiveControlTask {
		value, err := u.Store.LoadConfig()
		if err != nil {
			return UninstallResult{}, fmt.Errorf("load control task identity: %w", err)
		}
		controlTaskID = value.ControlTaskID
	}
	if err := ValidateManagedFile(u.Paths.Agents); err != nil {
		return UninstallResult{}, fmt.Errorf("validate AGENTS.md: %w", err)
	}
	if err := ValidateManagedFile(u.Paths.Skill); err != nil {
		return UninstallResult{}, fmt.Errorf("validate skill: %w", err)
	}
	if err := rejectSymlinkComponents(u.Paths.Binary); err != nil {
		return UninstallResult{}, err
	}
	if err := u.Scheduler.Remove(ctx); err != nil {
		return UninstallResult{}, fmt.Errorf("remove scheduler: %w", err)
	}
	if hadLaunchAgent || schedulerLoaded {
		resources = append(resources, "launchagent")
	}
	if err := DeleteManagedBlock(u.Paths.Agents); err != nil {
		return UninstallResult{}, fmt.Errorf("remove AGENTS.md block: %w", err)
	}
	if hadAgents {
		resources = append(resources, "agents")
	}
	if err := DeleteManagedBlock(u.Paths.Skill); err != nil {
		return UninstallResult{}, fmt.Errorf("remove skill block: %w", err)
	}
	if hadSkill {
		resources = append(resources, "skill")
	}
	if err := removeManagedFile(u.Paths.Binary); err != nil {
		return UninstallResult{}, fmt.Errorf("remove binary: %w", err)
	}
	if hadBinary {
		resources = append(resources, "binary")
	}
	archivedControlTask := false
	if archiveControlTask {
		changed, err := u.ControlTasks.ArchiveControlTask(ctx, controlTaskID)
		if err != nil {
			return UninstallResult{}, fmt.Errorf("archive control task: %w", err)
		}
		archivedControlTask = changed
		if changed {
			resources = append(resources, "control_task")
		}
	}
	if err := lock.Close(); err != nil {
		return UninstallResult{}, fmt.Errorf("release uninstall lock: %w", err)
	}
	lockHeld = false
	if err := rejectSymlinkComponents(u.Paths.StateDirectory); err != nil {
		return UninstallResult{}, err
	}
	if err := os.RemoveAll(u.Paths.StateDirectory); err != nil {
		return UninstallResult{}, fmt.Errorf("delete state: %w", err)
	}
	if hadState {
		resources = append(resources, "state")
	}
	return UninstallResult{ArchivedControlTask: archivedControlTask, DeletedState: hadState, Preview: preview, Changed: len(resources) > 0, Resources: resources}, nil
}

func acquireUninstallLock(ctx context.Context, store Store, wait, retryInterval time.Duration) (Lock, error) {
	deadline := time.Now().Add(wait)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("%w after waiting %s; rerunning uninstall is safe", ErrHeartbeatInFlight, wait)
		}
		lock, err := store.AcquireLock()
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, state.ErrLocked) {
			return nil, err
		}
		retry := min(retryInterval, time.Until(deadline))
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func managedFileHasBlock(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_, _, found, err := managedBlockBounds(data)
	return err == nil && found
}

func removeManagedFile(path string) error {
	if err := rejectSymlinkComponents(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrUnsafeManagedPath, path)
	}
	return os.Remove(path)
}
