package titleplan

import (
	"testing"

	"github.com/ericlitman/threadbear/internal/output"
)

func TestDispatchIsRetiredAndFailClosed(t *testing.T) {
	got := (Service{}).Dispatch().(output.TitleDispatchResult)
	if got.Allow || got.Disposition != "retired" {
		t.Fatalf("dispatch = %+v", got)
	}
}
