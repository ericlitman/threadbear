package titleplan

import "github.com/ericlitman/threadbear/internal/output"

type Service struct{}

func (Service) Dispatch() output.Result {
	return output.TitleDispatchResult{Allow: false, Disposition: "retired"}
}
