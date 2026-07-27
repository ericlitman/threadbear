package install

import (
	"regexp"
	"strings"

	"github.com/ericlitman/threadbear/assets"
)

// welcomeText sits beside the five-row welcome art. Each line stays short
// enough that art, gutter, and text fit an 80-column terminal.
var welcomeText = []string{
	"Hi! I'm ThreadBear, the Codex thread wrangler",
	"by Eric Litman. I run quietly as a LaunchAgent,",
	"keeping your threads titled, tidy, and archived",
	"while barely sipping tokens. If I help you out,",
	"share me, star me on GitHub. Go do great things!",
}

var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func visibleWidth(line string) int {
	return len([]rune(ansiSequence.ReplaceAllString(line, "")))
}

// WelcomeBanner composites the embedded art with the intro text. The art's
// final line carries only cursor-restore sequences and is appended untouched
// so the terminal is always left in a sane state.
func WelcomeBanner() string {
	lines := strings.Split(strings.TrimRight(assets.WelcomeArt, "\n"), "\n")
	rows := lines
	var tail string
	if len(lines) > len(welcomeText) {
		rows = lines[:len(welcomeText)]
		tail = strings.Join(lines[len(welcomeText):], "\n")
	}
	width := 0
	for _, row := range rows {
		if w := visibleWidth(row); w > width {
			width = w
		}
	}
	var builder strings.Builder
	builder.WriteString("\n")
	for index, row := range rows {
		builder.WriteString(row)
		builder.WriteString(strings.Repeat(" ", width-visibleWidth(row)+2))
		if index < len(welcomeText) {
			builder.WriteString(welcomeText[index])
		}
		builder.WriteString("\n")
	}
	if tail != "" {
		builder.WriteString(tail)
		builder.WriteString("\n")
	}
	return builder.String()
}
