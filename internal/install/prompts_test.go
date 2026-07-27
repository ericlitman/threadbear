package install

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/tokens"
)

func TestTTYPrompterDefaultsCustomAndSingleConfirmation(t *testing.T) {
	input := strings.NewReader(strings.Repeat("\n", 10) + "yes\n")
	var output bytes.Buffer
	prompt := NewTTYPrompter(input, &output)
	got, err := prompt.Collect(DefaultPreferences())
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultPreferences() {
		t.Fatalf("preferences=%+v", got)
	}
	if err := prompt.ShowPreview(Preview{Operation: "install", Lines: []string{"all effects"}}); err != nil {
		t.Fatal(err)
	}
	confirmed, err := prompt.Confirm(true)
	if err != nil || !confirmed {
		t.Fatalf("confirmed=%t error=%v", confirmed, err)
	}
	if strings.Count(output.String(), "Apply exactly this preview?") != 1 || strings.Count(output.String(), "ThreadBear install preview") != 1 {
		t.Fatalf("output=%q", output.String())
	}

	customInput := strings.NewReader("60\nno\nno\n30\nno\nend\nyes\ngpt-custom\nhigh\n9000\n")
	custom, err := NewTTYPrompter(customInput, &bytes.Buffer{}).Collect(DefaultPreferences())
	if err != nil {
		t.Fatal(err)
	}
	if custom.HeartbeatSeconds != 60 || custom.AutoUpdateEnabled || custom.ArchiveEnabled || custom.ArchiveAfterDays != 30 || custom.RenameEnabled || custom.TokenDisplay != tokens.PositionEnd || !custom.AgentsEnabled || custom.ClassifierModel != "gpt-custom" || custom.ClassifierEffort != config.EffortHigh || custom.ClassifierContextBudgetBytes != 9000 {
		t.Fatalf("custom=%+v", custom)
	}
}

func TestTTYPrompterShowsPlainMessage(t *testing.T) {
	var output bytes.Buffer
	prompt := NewTTYPrompter(strings.NewReader(""), &output)
	message := "Thanks for using ThreadBear. I'd love any feedback on why this wasn't for you. Drop me an email at eric@litman.org if you're open to sharing. Now, on to the uninstall!"
	if err := prompt.ShowMessage(message); err != nil {
		t.Fatal(err)
	}
	if output.String() != message+"\n\n" {
		t.Fatalf("output=%q", output.String())
	}
}
