# Uninstall title restore plan

2026-07-27. Scope: give `threadbear uninstall` an opt-in pass that removes ThreadBear's status emoji from **active (unarchived)** task titles before removal. Amends R39/U8, which said uninstall leaves titles untouched; untouched stays the default, so the amendment is opt-in only.

## Behavior

- **Interactive:** a third retention question, after control-task and state: `Remove ThreadBear status emoji from active task titles` (default `no`).
- **Noninteractive:** new uninstall-only flag `--restore-titles`, validated alongside `--archive-control-task` and `--delete-state`.
- **Preview:** one new line, `restore active task titles: %t`, and `titles` added to the previewed effects list.
- **Ordering:** the title pass runs first, inside the existing lifecycle-locked section, before the scheduler or any file is removed. The shared lock already serializes against heartbeats, so nothing re-prepends mid-pass.
- **Failure policy:** because the pass runs first, a wholesale failure — config unreadable, App Server won't start, inventory unreadable — aborts the uninstall with nothing removed, and re-running is idempotent since already-restored titles no-op. An individual `SetTitle` failure is counted and reported but does not block removal: the leftover is one title, fixable with a hand rename, not worth trapping the user in a retry loop.
- **Result:** `titles` appears in `resources` when at least one title changed; `UninstallResult`/`LifecycleResult` gain `restored_titles` and `failed_titles` counts (omitempty in JSON).

## Restoration rule

For each inventory row with `Archived == false` (the inventory read already excludes the control task):

1. Title does not start with one of the seven canonical prefixes (`⏳ 🚨 🙋 🤖 ➡️ ✅ ❔`) → skip; nothing of ours to remove.
2. A state `TaskRecord` exists whose `LastAppliedTitle` equals the current title → write `DurableSubject` (falling back to the existing owned-subject derivation when empty). Full undo: emoji, any managed token display, and the managed `→ action` all go.
3. Otherwise → write `stripOwnedToken(stripStatusPrefixes(title), record.ManagedTokenDisplay, record.ManagedTokenPosition)` — the same subject recovery `Reconcile` already applies to edited titles. Emoji and any recorded token display are removed; the rest is kept verbatim, including any `→` text, because we cannot prove we wrote it.
4. A result that would be empty (title was only emoji) → skip rather than write an empty title.

The pass reads the inventory once, computes every restoration, and writes — no per-write revalidation. The heartbeat revalidates because it runs unattended with minutes between capture and write; here the lifecycle lock already excludes the only other writer, so the sole remaining race is a hand rename in Codex Desktop during the few seconds the pass runs, accepted for a user-invoked one-shot command.

Known tradeoff: a hand-typed canonical prefix is indistinguishable from ours when state does not match. The status convention reserves those seven prefixes for ThreadBear, and the pass is opt-in, so stripping them is acceptable.

Why active-only: archived tasks are historical record; rewriting them churns revisions on hidden items for no operational benefit, and the request is scoped to active tasks. State is not rewritten after restoration — it is either deleted moments later or left as diagnostic history; on a later reinstall a stale `LastAppliedTitle` simply fails the exact-match test and reconciliation falls back to prefix stripping, which is correct.

## Implementation

- `internal/title/title.go` — add `Restore(record state.TaskRecord, current string) (string, bool)` implementing the rule above; reuses `stripStatusPrefixes`, `stripOwnedToken`, and the `ownedSubject` derivation. Zero-value record means "no state entry" (and makes `stripOwnedToken` a no-op).
- `internal/install/uninstall.go` — `UninstallRequest.RestoreTitles`; a `TitleRestorer` dependency (`RestoreActiveTitles(ctx, controlTaskID string) (restored, failed int, err error)`) mirroring the `ControlTasks` seam so `install` stays free of codex imports; the third `Choose`; the preview line; hoist the existing `LoadConfig` so the control-task ID serves both the archive choice and the pass; run the pass first under the lock and abort only on its error; extend both no-op early-return guards so `--restore-titles` still runs when nothing else is left to remove.
- `internal/app/app.go`, `internal/app/install_lifecycle.go` — `Request.RestoreTitles`, uninstall-only validation, mapping into `install.UninstallRequest`, the counts on the lifecycle result.
- `cmd/threadbear/main.go` — register `--restore-titles`; a ~20-line `restoreActiveTitles` loop beside `ensureControlTask`/`archiveControlTask` (load state, read inventory once, per-row `title.Restore`, `SetTitle`, count restored/failed) behind a small client interface like `controlTaskClient`; an adapter (state store + `lazyInventory` + `appServers.open`) wired into `uninstallFactory`; add `titles` to the noninteractive preview effects.

## Tests

- `internal/title`: exact-match restores the durable subject; empty subject falls back to the owned derivation; non-matching titles strip prefixes only; doubled prefixes; `➡️` with variation selector; recorded token displays removed in both start and `· out` end positions, and left alone when unrecorded; prefix-only title skipped; `🧵🐻` control title and non-canonical user emoji (`🚀 Ship it`) untouched.
- `internal/install`: choice defaults to `no`; preview line and ordering (pass before scheduler removal); a restorer error aborts with LaunchAgent, binary, managed blocks, and state all still present; reported title failures do not block removal; `titles` in resources; option off preserves today's behavior byte-for-byte; option on with state already deleted aborts with a clear error.
- `cmd/threadbear`: the loop skips archived rows, counts a `SetTitle` failure and finishes, and surfaces inventory/App Server failure as an error (fakes beside the existing control-task tests); `--restore-titles` parses on uninstall and is rejected elsewhere.

## Docs

- `README.md` command table: uninstall row mentions title retention choice.
- `INSTALL.md` uninstall section: the third question, `--restore-titles` in the noninteractive example, and that omitting it leaves titles alone.
- `docs/compatibility.md`: "Existing task titles and archives are left alone" becomes "left alone by default; `--restore-titles` removes ThreadBear status emoji from active tasks, and archived tasks are never rewritten."

## Out of scope

No standalone title-restore command; no archived-task rewriting; no preview counts (the preview line is static); no state rewrites after restoration; no per-write revalidation or cycle-checkpoint machinery (the heartbeat needs those for unattended minutes-long windows; a one-shot foreground pass does not); no changes to managed AGENTS/skill asset text.
