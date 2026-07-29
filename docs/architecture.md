# ThreadBear architecture

ThreadBear is a single pure-Go macOS binary run by a user LaunchAgent. Its design separates cheap, local certainty from the small set of changed tasks that genuinely need semantic interpretation.

## Components

- **Heartbeat runner:** inventories every managed unarchived Codex Desktop task, excluding the control task.
- **Deterministic resolver:** applies runtime, structured-error, automation, interruption, and valid footer evidence in precedence order.
- **Ephemeral classifier:** sends only unresolved changed tasks to fresh App Server sessions using the configured model and effort (Luna medium by default). Sessions are non-persisted and never append to the control task.
- **Mutation layer:** performs safe archives directly, but stages exact revision-guarded title plans for supported Codex-hosted native application so unrelated cycle commits never wait on Desktop repaint.
- **State store:** atomically records the last completed snapshot, task classifications, durable pending title plans, separate native/canonical outcomes, archive records, retries, update checks, and delivered notice versions.
- **Control task:** one persistent task titled `🧵🐻 ThreadBear 🐻🧵` used for actionable notices, not classifier history.

## Heartbeat flow

1. Acquire the shared lifecycle lock.
2. Read the complete local inventory and compare it with the committed snapshot.
3. If nothing changed and no update check or version-drift work is due, exit without starting App Server, invoking a classifier, mutating state, or writing output.
4. Resolve changed tasks from deterministic evidence and read only the new rollout tail needed for the cumulative output-token figure. Mechanically settled tasks never reach Luna.
5. Pack unresolved latest turns into context-safe ephemeral calls. A previous turn is requested only for tasks that return insufficient evidence; complete messages are not clipped.
6. Revalidate archives before direct mutation and stage title plans with the captured revision/title guard. The heartbeat never calls detached `thread/name/set`; a pending title defers only the same task's archive and does not block unrelated archives, updates, classifications, or sibling commits.
7. Commit successful siblings and the captured snapshot atomically. Failed operations retain conservative state and bounded retry metadata.
8. When due, compare release metadata. With auto-update enabled, install a newer release through the verified replacement path; with it disabled, retain the once-daily notice-only behavior. After any version change, the new binary reconciles managed guidance and stages one changelog-backed control-task announcement.

Classifier cost depends on unresolved changed work, not total task count or control-task history.

## Status and title ownership

ThreadBear uses seven canonical states. Titles have the shape:

```text
EMOJI durable subject → concise next action
```

The action is omitted when none is warranted. The optional token display uses cumulative output tokens from the last rollout `token_count` event and renders a two-significant-figure magnitude at the start or labeled end of the managed title. ThreadBear caches the rollout path and last-read offset/size, so an unchanged rollout is not read again.

ThreadBear strips existing canonical status prefixes, preserves user-edited subjects, and records its last applied title plus the exact token segment it owns. It can update or remove its prefix, token figure, and action without consuming user ownership. A Codex-hosted native call can report success, but that is distinct from canonical persistence. The next inventory settles canonical persistence and exact ThreadBear ownership. Rendered Desktop accessibility remains a separate release canary and is never inferred from SQLite, `list_threads`, or native success.

Only `complete` tasks can be auto-archived. Running, blocked, needs-input, automation, next-steps, and unknown tasks remain active regardless of age. A manual unarchive or `~/.local/bin/threadbear restore TASK_ID` starts a new inactivity grace period.

## Scheduler

The user LaunchAgent is `org.litman.threadbear` and runs `~/.local/bin/threadbear heartbeat`. Its plist uses `StartInterval` (default 300 seconds), `ProcessType=Background`, `KeepAlive=false`, and explicit `HOME`, `CODEX_HOME`, sanitized `PATH`, and `LC_ALL=C` values.

The interval is approximate. macOS does not replay runs missed during sleep, and overlapping launches are not used to catch up while a prior run is active.

## Files and privacy

| Resource | Path | Intended mode/handling |
|---|---|---|
| Binary | `~/.local/bin/threadbear` | regular executable, `0700` |
| State directory | `~/.local/share/threadbear` | private directory, `0700` |
| Config/state | `~/.local/share/threadbear/config.json`, `state.json` | private atomic files, `0600` |
| Logs | `~/.local/share/threadbear/logs/` | task IDs, counts, transitions, errors only |
| LaunchAgent | `~/Library/LaunchAgents/org.litman.threadbear.plist` | private regular file, `0600` |
| Global guidance | `${CODEX_HOME:-~/.codex}/AGENTS.md` | one identifiable managed block; ThreadBear writes `0600` |
| Skill | `${CODEX_HOME:-~/.codex}/skills/threadbear/SKILL.md` | one identifiable managed block, `0600` |

ThreadBear never logs message bodies, classifier payloads, inherited environment dumps, or private benchmark corpus content. Subprocesses receive an explicit minimal environment.

## Safety boundaries

ThreadBear does not edit `.codex-global-state.json` or private sidebar caches; click through Codex Desktop or organize projects/folders/pins; create visible classifier tasks; bypass Codex approval or weaken verified update gates; keep automatic rollback data; or auto-archive unfinished work.

Release binaries target `darwin/arm64` and `darwin/amd64` with `CGO_ENABLED=0` and need no Go, cgo, Python, Node.js, or other end-user runtime.

## Hosted title actuator

`threadbear title-plan` is strict JSON only. `--wait TASK_ID` waits a bounded time for the source turn to become terminal and returns one exact guarded plan; `--batch` returns all ready plans; `--report` records aggregate native success/failure without claiming canonical persistence or rendered verification. Guided installation uses direct native batches inside one `functions.exec`. The fallback is one projectless Luna/medium self-archiving actuator using only delegated source identity. The persistent master remains reserved for help, lifecycle, notices, decisions, and exceptional recovery.
