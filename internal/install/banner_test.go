package install

import (
	"strings"
	"testing"
)

func TestWelcomeBannerFitsEightyColumns(t *testing.T) {
	banner := WelcomeBanner()
	lines := strings.Split(strings.TrimRight(banner, "\n"), "\n")
	if len(lines) < len(welcomeText) {
		t.Fatalf("banner has %d lines", len(lines))
	}
	joined := strings.Join(lines, "\n")
	for _, text := range welcomeText {
		if !strings.Contains(joined, text) {
			t.Fatalf("banner missing text line %q", text)
		}
		if strings.Contains(text, "—") {
			t.Fatalf("em dash in welcome text %q", text)
		}
	}
	for index, line := range lines {
		if width := visibleWidth(line); width > 80 {
			t.Fatalf("line %d visible width %d exceeds 80", index, width)
		}
	}
}

func TestWelcomeNoticeCarriesSettingsAndChatInstructions(t *testing.T) {
	preferences := DefaultPreferences()
	notice := welcomeNotice("1.2.3", preferences)
	for _, want := range []string{
		"ThreadBear 1.2.3",
		"every 300 seconds",
		"after 14 quiet days",
		"gpt-5.6-luna",
		"tell me here in this chat",
		"threadbear configure",
	} {
		if !strings.Contains(notice, want) {
			t.Fatalf("welcome notice missing %q\n%s", want, notice)
		}
	}
	if strings.Contains(notice, "—") {
		t.Fatal("em dash in welcome notice")
	}
}
