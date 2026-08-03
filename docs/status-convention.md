# Status footer convention

Every terminal Codex response under ThreadBear guidance ends with exactly one of these forms:

```text
🧵🐻 complete
🧵🐻 next steps (you): approve the release plan
🧵🐻 next steps (agent): implement the approved plan
🧵🐻 next steps (external): review the security exception
🧵🐻 needs input (you): choose the release region
🧵🐻 blocked (external): restore the signing service
🧵🐻 automation
```

The footer is the final non-empty line, is not quoted or duplicated, and uses a concrete multiword action when an owner is present. `needs input` belongs to the user, `blocked` belongs to an external condition, and `next steps` may belong to the user, agent, or an external actor.

The same exact line is passed to the native current-task title setter immediately before the final response. ThreadBear maps it deterministically:

| Footer | Visible title |
| --- | --- |
| `complete` | `✅ <subject>` |
| `next steps (…)` | `➡️ <subject> → <action>` |
| `needs input (you)` | `🙋 <subject> → <action>` |
| `blocked (external)` | `🚨 <subject> → <action>` |
| `automation` | `🤖 <subject>` |

At turn start, `⏳ ThreadBear is working: <concise subject>` maps to `⏳ <subject>`. The seed is used only when a fresh task still exposes its raw first message; established subjects and user renames win. `❔` is reserved for legacy items that remain unknown during installation; ordinary turns do not emit it. The migration controller requests that state with the exact native title input `❔ ThreadBear could not classify`; there is no compact unknown footer.
