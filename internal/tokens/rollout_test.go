package tokens

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRolloutUsesLastCumulativeTokenCount(t *testing.T) {
	path := filepath.Join("testdata", "rollout-tail.jsonl")

	got, err := ReadRollout(path, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.OutputTokens != 1_600_123 || got.TotalTokens != 433_000_000 {
		t.Fatalf("ReadRollout() = %+v", got)
	}
	if got.RolloutPath != path || got.Offset == 0 || got.Offset != got.Size {
		t.Fatalf("cursor = %+v", got)
	}
}

func TestReadRolloutReusesUnchangedSnapshotWithoutOpeningFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	content := []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"output_tokens\":1200,\"total_tokens\":340000}}}}\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	first, err := ReadRollout(path, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })

	second, err := ReadRollout(path, first)
	if err != nil {
		t.Fatalf("unchanged rollout was reopened: %v", err)
	}
	if second != first {
		t.Fatalf("unchanged snapshot = %+v, want %+v", second, first)
	}
}

func TestReadRolloutConsumesOnlyAppendedEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	firstEvent := []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"output_tokens\":1200,\"total_tokens\":340000}}}}\n")
	if err := os.WriteFile(path, firstEvent, 0600); err != nil {
		t.Fatal(err)
	}
	first, err := ReadRollout(path, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	appended := []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"output_tokens\":1600000,\"total_tokens\":433000000}}}}\n")
	if _, err := file.Write(appended); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := ReadRollout(path, first)
	if err != nil {
		t.Fatal(err)
	}
	if second.OutputTokens != 1_600_000 || second.TotalTokens != 433_000_000 || second.Offset <= first.Offset {
		t.Fatalf("appended snapshot = %+v, first = %+v", second, first)
	}
}

func TestReadRolloutClearsUsageForMalformedAppendedTokenCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	firstEvent := []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"output_tokens\":1200,\"total_tokens\":340000}}}}\n")
	if err := os.WriteFile(path, firstEvent, 0600); err != nil {
		t.Fatal(err)
	}
	first, err := ReadRollout(path, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	malformed := []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{}}}\n")
	if _, err := file.Write(malformed); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := ReadRollout(path, first)
	if err != nil {
		t.Fatal(err)
	}
	if second.Found || second.OutputTokens != 0 || second.TotalTokens != 0 {
		t.Fatalf("malformed appended token event retained stale usage: %+v", second)
	}
	if second.Offset != second.Size || second.Size <= first.Size {
		t.Fatalf("malformed appended cursor = %+v, first = %+v", second, first)
	}
}

func TestReadRolloutResumesPartialTokenCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	firstEvent := []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"output_tokens\":1200,\"total_tokens\":340000}}}}\n")
	if err := os.WriteFile(path, firstEvent, 0600); err != nil {
		t.Fatal(err)
	}
	first, err := ReadRollout(path, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	partial := []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"output_tokens\":1600000")
	if _, err := file.Write(partial); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	pending, err := ReadRollout(path, first)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Found || pending.OutputTokens != 0 || pending.TotalTokens != 0 {
		t.Fatalf("partial token event retained stale usage: %+v", pending)
	}
	if pending.Offset != int64(len(firstEvent)) || pending.Size <= pending.Offset {
		t.Fatalf("partial token cursor = %+v", pending)
	}

	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	unchanged, err := ReadRollout(path, pending)
	if err != nil {
		t.Fatalf("unchanged partial rollout was reopened: %v", err)
	}
	if unchanged != pending {
		t.Fatalf("unchanged partial snapshot = %+v, want %+v", unchanged, pending)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}

	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	remainder := []byte(",\"total_tokens\":433000000}}}}\n")
	if _, err := file.Write(remainder); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	complete, err := ReadRollout(path, pending)
	if err != nil {
		t.Fatal(err)
	}
	if !complete.Found || complete.OutputTokens != 1_600_000 || complete.TotalTokens != 433_000_000 {
		t.Fatalf("completed token event = %+v", complete)
	}
	if complete.Offset != complete.Size || complete.Size <= pending.Size {
		t.Fatalf("completed token cursor = %+v, pending = %+v", complete, pending)
	}
}

func TestReadRolloutWithoutTokenCountHasNoFigure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":\"synthetic\"}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRollout(path, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Found || got.OutputTokens != 0 {
		t.Fatalf("ReadRollout() = %+v", got)
	}
}

func TestReadRolloutDoesNotFallBackPastMalformedLatestTokenCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	content := []byte(
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"output_tokens\":1200,\"total_tokens\":340000}}}}\n" +
			"{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{}}}\n",
	)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRollout(path, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Found {
		t.Fatalf("malformed latest token event fell back to stale usage: %+v", got)
	}
}

func TestReadRolloutRejectsTokenCountWithoutOutputTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	content := []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"total_tokens\":340000}}}}\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadRollout(path, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Found || got.OutputTokens != 0 || got.TotalTokens != 0 {
		t.Fatalf("token event without output_tokens produced usage: %+v", got)
	}
}

func TestFormatUsesTwoSignificantFigures(t *testing.T) {
	tests := map[uint64]string{
		0:           "0",
		999:         "999",
		1_200:       "1.2k",
		12_500:      "13k",
		340_000:     "340k",
		999_999:     "1m",
		1_600_000:   "1.6m",
		433_000_000: "430m",
	}
	for value, want := range tests {
		t.Run(want, func(t *testing.T) {
			if got := Format(value); got != want {
				t.Fatalf("Format(%d) = %q, want %q", value, got, want)
			}
		})
	}
}
