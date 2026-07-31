package assets

import _ "embed"

//go:embed AGENTS.threadbear.md
var AgentsManagedContent string

//go:embed skill/SKILL.md
var SkillManagedContent string

//go:embed help.txt
var HelpText string
