# Status footer convention

A terminal Codex response may end with exactly one of these lines:

```text
🧵🐻 complete
🧵🐻 next steps (you): approve the release plan
🧵🐻 next steps (agent): implement the approved plan
🧵🐻 next steps (external): review the security exception
🧵🐻 needs input (you): choose the release region
🧵🐻 blocked (external): restore the signing service
🧵🐻 automation
```

The footer must be the final non-empty line, cannot be quoted or duplicated,
and an owner-bearing action must contain a concrete multiword instruction.
ThreadBear accepts only the bindings shown above: `needs input` belongs to the
user, `blocked` belongs to an external condition, and `next steps` may belong
to the user, agent, or an external actor.

Exact accepted footers are deterministic. Legacy prose without an exact footer
remains unknown until live runtime state resolves it or unchanged ambiguity is
sent to Luna medium.
