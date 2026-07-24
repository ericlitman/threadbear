package install

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/internal/config"
)

func TestTTYPrompterDefaultsCustomAndSingleConfirmation(t *testing.T) {
	input := strings.NewReader("\n\n\n\n\n\n\n\n yes\n")
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
	confirmed, err := prompt.Confirm()
	if err != nil || !confirmed {
		t.Fatalf("confirmed=%t error=%v", confirmed, err)
	}
	if strings.Count(output.String(), "Apply exactly this preview?") != 1 || strings.Count(output.String(), "ThreadBear install preview") != 1 {
		t.Fatalf("output=%q", output.String())
	}

	customInput := strings.NewReader("60\nno\n30\nno\nyes\ngpt-custom\nhigh\n9000\n")
	custom, err := NewTTYPrompter(customInput, &bytes.Buffer{}).Collect(DefaultPreferences())
	if err != nil {
		t.Fatal(err)
	}
	if custom.HeartbeatSeconds != 60 || custom.ArchiveEnabled || custom.ArchiveAfterDays != 30 || custom.RenameEnabled || !custom.AgentsEnabled || custom.ClassifierModel != "gpt-custom" || custom.ClassifierEffort != config.EffortHigh || custom.ClassifierContextBudgetBytes != 9000 {
		t.Fatalf("custom=%+v", custom)
	}
}
