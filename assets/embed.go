package assets

import _ "embed"

//go:embed org.litman.threadbear.plist.tmpl
var LaunchAgentPlistTemplate string

//go:embed AGENTS.threadbear.md
var AgentsManagedContent string

//go:embed skill/SKILL.md
var SkillManagedContent string

//go:embed both5_safe.ans
var WelcomeArt string
