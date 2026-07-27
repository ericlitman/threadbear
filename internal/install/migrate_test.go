package install

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/state"
)

func TestDecodeThreadWatchStrictMapping(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"controllerThreadId":"controller-1","cycleCompletedAtMs":1700000000123,"retryIds":["task-2"],"threads":{"task-1":{"activityAtMs":1690000000000,"title":"✅ shipped"},"task-2":{"activityAtMs":1695000000000,"title":"🚨 blocked"},"task-3":{"activityAtMs":1697000000000,"title":"plain title"}}}`)
	migration, err := DecodeThreadWatch(data)
	if err != nil {
		t.Fatal(err)
	}
	if migration.ControlTaskID != "controller-1" {
		t.Fatalf("control=%q", migration.ControlTaskID)
	}
	if got := migration.State.LastCompletedHeartbeat; got == nil || !got.Equal(time.UnixMilli(1700000000123)) {
		t.Fatalf("heartbeat=%v", got)
	}
	if migration.State.Tasks["task-1"].Status != state.StatusComplete || migration.State.Tasks["task-2"].Status != state.StatusBlocked || migration.State.Tasks["task-3"].Status != state.StatusUnknown {
		t.Fatalf("tasks=%+v", migration.State.Tasks)
	}
	retry := migration.State.Tasks["task-2"].Retry
	if retry == nil || retry.Operation != "legacy_classification" || retry.ErrorCode != "threadwatch_retry" || retry.Attempts != 1 {
		t.Fatalf("retry=%+v", retry)
	}
	if migration.State.Tasks["task-1"].CapturedTitle != "✅ shipped" || migration.State.Tasks["task-1"].LastSubstantiveActivity.UnixMilli() != 1690000000000 {
		t.Fatalf("task=%+v", migration.State.Tasks["task-1"])
	}
}

func TestDecodeThreadWatchRejectsUnknownMissingAndNewerSchema(t *testing.T) {
	cases := []string{
		`{"schemaVersion":99,"controllerThreadId":"c","cycleCompletedAtMs":1,"retryIds":[],"threads":{}}`,
		`{"schemaVersion":1,"controllerThreadId":"c","cycleCompletedAtMs":1,"retryIds":[],"threads":{},"preference":true}`,
		`{"schemaVersion":1,"controllerThreadId":"c","cycleCompletedAtMs":1,"retryIds":[],"threads":{"t":{"activityAtMs":1,"title":"x","extra":1}}}`,
		`{"schemaVersion":1,"controllerThreadId":"c","cycleCompletedAtMs":1,"threads":{}}`,
		`{"schemaVersion":1,"controllerThreadId":"c","cycleCompletedAtMs":1,"retryIds":["missing"],"threads":{}}`,
	}
	for _, data := range cases {
		if _, err := DecodeThreadWatch([]byte(data)); !errors.Is(err, ErrInvalidThreadWatchState) {
			t.Fatalf("data=%s error=%v", data, err)
		}
	}
}

func TestDecodeThreadWatchAcceptsEverySupportedSchemaVersion(t *testing.T) {
	// ThreadWatch advanced to schemaVersion 4 with the same field shape; a
	// real install failed because only version 1 was accepted.
	for version := ThreadWatchSchemaVersion; version <= ThreadWatchMaxSchemaVersion; version++ {
		payload := fmt.Sprintf(`{"schemaVersion":%d,"controllerThreadId":"019f8f9f-77fb-7240-b9ae-7963527b9af3","cycleCompletedAtMs":1784983920350,"retryIds":[],"threads":{"019c9852-d1e4-73c1-b8b6-997b29f448ca":{"activityAtMs":1784983900000,"title":"a thread"}}}`, version)
		migration, err := DecodeThreadWatch([]byte(payload))
		if err != nil {
			t.Fatalf("schemaVersion %d: %v", version, err)
		}
		if migration.ControlTaskID != "019f8f9f-77fb-7240-b9ae-7963527b9af3" || len(migration.State.Tasks) != 1 {
			t.Fatalf("schemaVersion %d migrated %+v", version, migration)
		}
	}
	future := fmt.Sprintf(`{"schemaVersion":%d,"controllerThreadId":"019f8f9f-77fb-7240-b9ae-7963527b9af3","cycleCompletedAtMs":1784983920350,"retryIds":[],"threads":{}}`, ThreadWatchMaxSchemaVersion+1)
	if _, err := DecodeThreadWatch([]byte(future)); !errors.Is(err, ErrInvalidThreadWatchState) {
		t.Fatalf("future schema error=%v", err)
	}
}
