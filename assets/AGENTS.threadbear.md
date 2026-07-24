# ThreadBear status

End each terminal response with exactly one compact status line:

`🐻 STATUS · next (OWNER): ACTION`

STATUS is `complete`, `next steps`, `needs input`, `blocked`, or `automation`. OWNER is `you`, `agent`, `external`, or `none`.

Report the turn's actual disposition; do not invent or recommend work to populate this line. After finished work, use `complete · next (none): none` unless the substantive response already ends with one clear, concrete, warranted next step; generic offers, speculative possibilities, and mentions of recorded work do not qualify.
