# Architecture

ThreadBear is one executable and one versioned JSON state file. The production
path is a four-stage state machine:

1. **Scan.** Under one private flock, read the current Codex index and each
   changed rollout to a fixed complete-line boundary. Exact footers and
   structured errors become decisions; unresolved evidence records an exact
   path, boundary, digest, revision, and title.
2. **Resolve.** Release the lock. Read live App Server state for unresolved
   tasks. Active and waiting tasks resolve deterministically. Only evidence
   that remains byte-identical across a later pass may enter an ephemeral,
   read-only Luna medium call.
3. **Commit.** Reacquire the lock, reread the index and evidence, and discard
   any stale runtime or semantic result. A durable plan binds the evidence,
   full user-owned subject, status, action, expected title, desired title, and
   issuance epoch.
4. **Apply.** The retained task drains plans in fixed batches. Each operation
   revalidates current evidence and title, calls the supported native Desktop
   setter, checks its exact task-and-title result, and commits only after the
   Codex index exposes the desired title. Drift is preserved rather than
   overwritten.

Background heartbeats stop after the commit stage; they never write a title.
The native retained-task endpoint exposes durable plans in batches of eight.
Codex Desktop performs the supported setter; ThreadBear accepts a report only
after both the native result and current Codex task index match exactly.
The setter has no compare-and-set primitive, so a user rename in the narrow
interval after revalidation and before mutation cannot be made atomic.

`core.json` has an explicit format number and is written through a mode-0600
temporary file, `fsync`, rename, and directory `fsync`. The reset does not
reinterpret the former `config.json` or `state.json` schemas.
