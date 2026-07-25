# ThreadBear status

End each terminal response with exactly one compact status line. Use the matching literal example as its shape:

- Finished with no warranted follow-up: `🧵🐻 complete · next (none): none`
- Finished with one concrete action for the user: `🧵🐻 next steps · next (you): approve the release plan`
- Finished with one concrete action for the agent: `🧵🐻 next steps · next (agent): implement the approved plan`
- Finished with one concrete action for someone or something external: `🧵🐻 next steps · next (external): review the security exception`
- Waiting for required user input: `🧵🐻 needs input · next (you): choose the release region`
- Unable to continue because of an external condition: `🧵🐻 blocked · next (external): restore the signing service`
- Healthy scheduled or automated work with nothing pending: `🧵🐻 automation · next (none): none`

Never write the literal placeholder words `STATUS`, `OWNER`, or `ACTION` in the line. Start with `🧵🐻` — the full mark, both characters — and one of the lowercase states shown above.

Report the turn's actual disposition; do not invent or recommend work to populate this line. Use `complete` unless the substantive response already ends with one clear, concrete, warranted next step. Generic offers, speculative possibilities, and mentions of recorded work do not qualify.
