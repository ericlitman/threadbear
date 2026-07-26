//go:build integration

package status

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/codex/appserver"
)

func TestLiveLunaMediumCorpus(t *testing.T) {
	if os.Getenv("THREADBEAR_LIVE_EVAL") != "1" {
		t.Skip("set THREADBEAR_LIVE_EVAL=1 to run the operator-supplied corpus gate")
	}
	corpusPath := requiredLiveEvalPath(t, "THREADBEAR_LIVE_EVAL_CORPUS")
	authPath := requiredLiveEvalPath(t, "THREADBEAR_LIVE_AUTH_FILE")
	data, err := os.ReadFile(corpusPath)
	if err != nil {
		t.Fatalf("read THREADBEAR_LIVE_EVAL_CORPUS: %v", err)
	}
	corpus, err := decodeLiveEvalCorpus(data)
	if err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	auth, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("read THREADBEAR_LIVE_AUTH_FILE: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "auth.json"), auth, 0600); err != nil {
		t.Fatalf("copy disposable auth.json: %v", err)
	}
	process := appserver.DefaultProcessSpec(home)
	if executable := strings.TrimSpace(os.Getenv("THREADBEAR_LIVE_CODEX_BIN")); executable != "" {
		process.Path = executable
	}
	runs := liveEvalInt(t, "THREADBEAR_LIVE_EVAL_RUNS", 5)
	timeout := liveEvalDuration(t, "THREADBEAR_LIVE_EVAL_TIMEOUT", time.Duration(runs)*15*time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	caps, err := appserver.DiscoverCapabilities(ctx, process)
	if err != nil {
		t.Fatal(err)
	}
	client, err := appserver.Start(ctx, process, caps)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close App Server: %v", err)
		}
	}()

	classifier, err := NewClassifier(client, ClassifierConfig{
		Model:              liveEvalValue("THREADBEAR_LIVE_MODEL", "gpt-5.6-luna"),
		Effort:             liveEvalValue("THREADBEAR_LIVE_EFFORT", "medium"),
		ContextBudgetBytes: liveEvalInt(t, "THREADBEAR_LIVE_EVAL_CONTEXT_BYTES", 1<<20),
	})
	if err != nil {
		t.Fatal(err)
	}
	series, err := runLiveEvalSeries(ctx, corpus, classifier, runs)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(series, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("live eval series (%d runs):\n%s", series.Runs, encoded)
	t.Logf("caveat: %s", series.Caveat)
	if len(series.Flapping) > 0 {
		t.Logf(
			"flapping (non-gating): %d case(s) dangerous-wrong in fewer than %d of %d runs",
			len(series.Flapping), series.Threshold, series.Runs,
		)
	}
	// The gate fails only on SYSTEMATIC dangerous errors: a case wrong in a
	// dangerous direction, or unscoreable, in a majority of runs. Sub-majority
	// misses are sampling noise on a path that exposes no seed control; they
	// are reported above, not gated on (Verification Contract, 2026-07-26).
	if len(series.Systematic) > 0 || len(series.Unscoreable) > 0 {
		t.Fatalf(
			"release gate failed: systematic=%d unscoreable=%d (threshold %d of %d runs)",
			len(series.Systematic),
			len(series.Unscoreable),
			series.Threshold,
			series.Runs,
		)
	}
}

func requiredLiveEvalPath(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required when THREADBEAR_LIVE_EVAL=1", key)
	}
	if !filepath.IsAbs(value) {
		t.Fatalf("%s must be an absolute path", key)
	}
	return value
}

func liveEvalValue(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func liveEvalInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		t.Fatalf("%s must be a positive integer", key)
	}
	return parsed
}

func liveEvalDuration(t *testing.T, key string, fallback time.Duration) time.Duration {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		t.Fatalf("%s must be a positive duration", key)
	}
	return parsed
}
