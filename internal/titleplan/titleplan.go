package titleplan

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
)

type Store interface {
	LoadConfig() (config.Config, error)
	LoadState() (state.State, error)
	SaveState(state.State) error
	LoadCycle() (state.CycleCheckpoint, error)
	AcquireLock() (*state.Lock, error)
}
type Inventory interface {
	Inventory(context.Context, string) (codex.Inventory, error)
}
type Heartbeat interface {
	Run(context.Context, bool) (output.Result, error)
}
type SingleTaskPlanner interface {
	PlanTitle(context.Context, string) error
}
type TerminalWaiter interface {
	Wait(context.Context, string) error
}

type Service struct {
	Store     Store
	Inventory Inventory
	Heartbeat Heartbeat
	Planner   SingleTaskPlanner
	Waiter    TerminalWaiter
	Reports   io.Reader
	Now       func() time.Time
}
type NativeReport struct {
	OperationID   string `json:"operation_id"`
	TaskID        string `json:"task_id"`
	NativeSuccess *bool  `json:"native_success"`
	ErrorCode     string `json:"error_code,omitempty"`
}
type reportEnvelope struct {
	Reports []NativeReport `json:"reports"`
}

var errCycleInProgress = errors.New("cycle_in_progress")

const ChildPromptSentinel = "THREADBEAR_TITLE_ACTUATOR_V1"
const ChildSourcePlaceholder = "__THREADBEAR_SOURCE_UUID__"

const ChildActuatorProgram = `const result=await(async()=>{const s="__THREADBEAR_SOURCE_UUID__",o=x=>x!==null&&typeof x==="object"&&!Array.isArray(x),k=x=>Object.keys(x).sort().join(","),i=x=>typeof x==="string"&&x!==""&&x.trim()===x,q=x=>"'"+x.replace(/'/g,"'\\''")+"'",f=()=>({ok:false,error:"title_actuation_failed"}),a=new Set(["canonical_persisted","drifted","missing","native_succeeded_pending_canonical","no_op"]),z=new Set(["canonical_persisted","native_succeeded_pending_canonical","no_op"]),c=async x=>{let r;try{r=await tools.exec_command({cmd:x})}catch{return null}if(!o(r)||typeof r.output!=="string"||typeof r.exit_code!=="number"||r.exit_code!==0||"session_id"in r)return null;try{return JSON.parse(r.output)}catch{return null}},p=x=>o(x)&&k(x)==="desired_title,expected_revision,expected_title,operation_id,task_id"&&i(x.operation_id)&&i(x.task_id)&&i(x.expected_revision)&&typeof x.expected_title==="string"&&typeof x.desired_title==="string"&&x.desired_title!=="",d=x=>o(x)&&k(x)==="outcome,task_id"&&i(x.task_id)&&a.has(x.outcome),e=(x,m)=>{if(!o(x)||k(x)!=="dispositions,mode,plans,version"||x.version!==1||x.mode!==m||!Array.isArray(x.plans)||!Array.isArray(x.dispositions)||!x.plans.every(p)||!x.dispositions.every(d))return false;const n=new Set;for(const y of x.plans){if(n.has(y.task_id))return false;n.add(y.task_id)}const h=new Set;for(const y of x.dispositions){if(n.has(y.task_id)||h.has(y.task_id))return false;h.add(y.task_id)}return true},v=x=>{if(!o(x)||k(x)!=="accepted_ids,rejected_ids,version"||x.version!==1||!Array.isArray(x.accepted_ids)||!Array.isArray(x.rejected_ids)||!x.accepted_ids.every(i)||!x.rejected_ids.every(i))return false;const y=[...x.accepted_ids,...x.rejected_ids];return new Set(y).size===y.length};if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(s))return f();const w=await c("~/.local/bin/threadbear title-plan --json --wait "+q(s));if(!e(w,"wait"))return f();let g=false;if(w.plans.length===0)g=w.dispositions.every(x=>z.has(x.outcome));else{const t=[];let b=false;for(const x of w.plans){const r=await c("~/.local/bin/threadbear title-plan --json --operation "+q(x.operation_id));if(!e(r,"operation")||r.plans.length!==1||r.plans[0].operation_id!==x.operation_id){b=true;break}const y=r.plans[0],n={operation_id:y.operation_id,task_id:y.task_id,native_success:true};try{await tools.codex_app__set_thread_title({threadId:y.task_id,title:y.desired_title})}catch{n.native_success=false;n.error_code="native_set_failed"}t.push(n)}let r=null;if(t.length){const x=JSON.stringify({reports:t});r=await c("printf %s "+q(x)+" | ~/.local/bin/threadbear title-plan --json --report")}g=!b&&t.length>0&&v(r)&&r.rejected_ids.length===0&&r.accepted_ids.length===t.length&&r.accepted_ids.every(x=>t.some(y=>y.task_id===x))&&!t.some(x=>!x.native_success)}if(!g)return f();await tools.codex_app__set_thread_archived({archived:true});return{ok:true}})();text(JSON.stringify(result))`

const ChildActuatorLoader = `await(async()=>{const s="__THREADBEAR_SOURCE_UUID__",o=x=>x!==null&&typeof x==="object"&&!Array.isArray(x),k=x=>Object.keys(x).sort().join(","),f=()=>text(JSON.stringify({ok:false,error:"title_actuation_failed"}));if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(s))return f();let r,e;try{r=await tools.exec_command({cmd:"~/.local/bin/threadbear title-plan --json --actuator "+s})}catch{return f()}if(!o(r)||typeof r.output!=="string"||typeof r.exit_code!=="number"||r.exit_code!==0||"session_id"in r)return f();try{e=JSON.parse(r.output)}catch{return f()}if(!o(e)||k(e)!=="program,version"||e.version!==1||typeof e.program!=="string"||e.program==="")return f();await(0,eval)("(async()=>{"+e.program+"\n})()")})()`

const ChildPrompt = ChildPromptSentinel + `
In one model pass, set s = your lowercase canonical codex_delegation.source_thread_id; run unchanged in exactly one functions.exec. Never author, inspect, explain, retry, or recover.
` + ChildActuatorLoader + ``

func (s Service) Actuator(sourceID string) (output.Result, error) {
	if !canonicalUUID(sourceID) || strings.Count(ChildActuatorProgram, ChildSourcePlaceholder) != 1 {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_request"}, errors.New("title-plan actuator requires one lowercase canonical source UUID")
	}
	return output.TitleActuatorResult{Program: strings.Replace(ChildActuatorProgram, ChildSourcePlaceholder, sourceID, 1)}, nil
}

func (s Service) Plan(ctx context.Context, taskID, operationID string, batch, report, dispatch bool) (output.Result, error) {
	modes := 0
	if taskID != "" {
		modes++
	}
	if operationID != "" {
		modes++
	}
	if batch {
		modes++
	}
	if report {
		modes++
	}
	if dispatch {
		modes++
	}
	if strings.TrimSpace(taskID) != taskID || strings.TrimSpace(operationID) != operationID || modes != 1 {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_request"}, errors.New("title-plan requires exactly one strict mode")
	}
	if dispatch {
		return s.dispatch(), nil
	}
	if report {
		if s.Store == nil || s.Now == nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "dependency_unavailable"}, errors.New("title report dependencies unavailable")
		}
		return s.report()
	}
	if s.Store == nil || s.Inventory == nil || s.Now == nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "dependency_unavailable"}, errors.New("title-plan dependencies unavailable")
	}
	mode := "batch"
	if taskID != "" {
		mode = "wait"
	} else if operationID != "" {
		mode = "operation"
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "config_read_failed"}, err
	}
	if !cfg.RenameEnabled {
		return s.disabled(mode, taskID)
	}
	if taskID != "" && taskID == cfg.ControlTaskID {
		return output.TitlePlanResult{Mode: mode, Plans: []output.TitlePlanItem{}, Dispositions: disabledDispositions(taskID)}, nil
	}
	if taskID != "" {
		if s.Waiter == nil || s.Planner == nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "wait_unavailable"}, errors.New("single-task title planner unavailable")
		}
		if err := s.Waiter.Wait(ctx, taskID); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "wait_failed"}, err
		}
		if err := s.Planner.PlanTitle(ctx, taskID); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "planning_failed"}, err
		}
	} else if batch {
		if s.Heartbeat == nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "batch_unavailable"}, errors.New("batch heartbeat unavailable")
		}
		if _, err := s.Heartbeat.Run(ctx, false); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "planning_failed"}, err
		}
	} else if operationID == "" {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_request"}, errors.New("title-plan mode is invalid")
	}
	lock, err := s.Store.AcquireLock()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "title_plan_locked"}, err
	}
	defer lock.Close()
	cfg, err = s.Store.LoadConfig()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "config_read_failed"}, err
	}
	if !cfg.RenameEnabled {
		return output.TitlePlanResult{Mode: mode, Plans: []output.TitlePlanItem{}, Dispositions: disabledDispositions(taskID)}, nil
	}
	if err := refuseCycle(s.Store); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "cycle_in_progress"}, err
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_read_failed"}, err
	}
	observed, err := s.Inventory.Inventory(ctx, cfg.ControlTaskID)
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "inventory_failed"}, err
	}
	byID := make(map[string]codex.Task, len(observed.Tasks))
	for _, task := range observed.Tasks {
		byID[task.TaskID] = task
	}
	result := output.TitlePlanResult{Mode: mode, Plans: []output.TitlePlanItem{}, Dispositions: []output.TitlePlanDisposition{}}
	ids := make([]string, 0, len(committed.PendingTitlePlans))
	for id, plan := range committed.PendingTitlePlans {
		if taskID != "" && id != taskID {
			continue
		}
		if operationID != "" && plan.OperationID != operationID {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	changed := false
	now := s.Now().UTC()
	for _, id := range ids {
		plan, task := committed.PendingTitlePlans[id], byID[id]
		sameTitleRefresh := plan.ExpectedTitle == plan.DesiredTitle
		switch {
		case task.TaskID == "":
			delete(committed.PendingTitlePlans, id)
			result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: id, Outcome: "missing"})
			changed = true
		case task.Title == plan.DesiredTitle && (!sameTitleRefresh || plan.NativeOutcome == state.NativeTitleSucceeded):
			applyCanonical(&committed, plan, task)
			result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: id, Outcome: "canonical_persisted"})
			changed = true
		case task.Revision != plan.ExpectedRevision || task.Title != plan.ExpectedTitle:
			delete(committed.PendingTitlePlans, id)
			result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: id, Outcome: "drifted"})
			changed = true
		case plan.NativeOutcome == state.NativeTitleSucceeded && plan.NativeReportedAt != nil && !now.Before(plan.NativeReportedAt.Add(state.NativeTitleCanonicalTimeout)):
			plan.NativeOutcome, plan.NativeErrorCode, plan.NativeReportedAt = state.NativeTitleFailed, "canonical_not_persisted", &now
			committed.PendingTitlePlans[id] = plan
			result.Plans = append(result.Plans, titlePlanItem(plan))
			changed = true
		case plan.NativeOutcome == state.NativeTitleSucceeded:
			result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: id, Outcome: "native_succeeded_pending_canonical"})
		default:
			result.Plans = append(result.Plans, titlePlanItem(plan))
		}
	}
	if taskID != "" && len(ids) == 0 {
		result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: taskID, Outcome: "no_op"})
	}
	if changed {
		committed.Generation++
		if err := s.Store.SaveState(committed); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_write_failed"}, err
		}
	}
	return result, nil
}

func (s Service) dispatch() output.TitleDispatchResult {
	noOp := func(disposition string) output.TitleDispatchResult {
		return output.TitleDispatchResult{Allow: false, Disposition: disposition}
	}
	sourceID := os.Getenv("CODEX_THREAD_ID")
	if sourceID == "" {
		return noOp("source_missing")
	}
	if !canonicalUUID(sourceID) {
		return noOp("source_invalid")
	}
	if s.Store == nil {
		return noOp("config_unavailable")
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return noOp("config_unavailable")
	}
	if err := cfg.Validate(); err != nil || !canonicalUUID(cfg.ControlTaskID) {
		return noOp("config_invalid")
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return noOp("state_unavailable")
	}
	if err := committed.Validate(); err != nil {
		return noOp("state_invalid")
	}
	if sourceID == cfg.ControlTaskID {
		return noOp("control_task")
	}
	if !cfg.RenameEnabled {
		return noOp("rename_disabled")
	}
	if !cfg.AgentsEnabled {
		return noOp("agents_disabled")
	}
	return output.TitleDispatchResult{
		Allow:       true,
		Disposition: "dispatch",
		Child: &output.TitleDispatchChild{
			Model:    "gpt-5.6-luna",
			Thinking: "medium",
			Target: output.TitleDispatchTarget{
				Type:          "projectless",
				DirectoryName: "threadbear-title-actuator",
			},
			Prompt: ChildPrompt,
		},
	}
}

func canonicalUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}

func titlePlanItem(plan state.PendingTitlePlan) output.TitlePlanItem {
	return output.TitlePlanItem{OperationID: plan.OperationID, TaskID: plan.TaskID, ExpectedRevision: plan.ExpectedRevision, ExpectedTitle: plan.ExpectedTitle, DesiredTitle: plan.DesiredTitle}
}

func disabledDispositions(taskID string) []output.TitlePlanDisposition {
	if taskID == "" {
		return []output.TitlePlanDisposition{}
	}
	return []output.TitlePlanDisposition{{TaskID: taskID, Outcome: "no_op"}}
}

func refuseCycle(store Store) error {
	_, err := store.LoadCycle()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return errCycleInProgress
}

func (s Service) disabled(mode, taskID string) (output.Result, error) {
	lock, err := s.Store.AcquireLock()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "title_plan_locked"}, err
	}
	defer lock.Close()
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "config_read_failed"}, err
	}
	result := output.TitlePlanResult{Mode: mode, Plans: []output.TitlePlanItem{}, Dispositions: disabledDispositions(taskID)}
	if cfg.RenameEnabled {
		return result, nil
	}
	if err := refuseCycle(s.Store); err != nil {
		if errors.Is(err, errCycleInProgress) {
			return result, nil
		}
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "cycle_read_failed"}, err
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_read_failed"}, err
	}
	if len(committed.PendingTitlePlans) == 0 {
		return result, nil
	}
	committed.PendingTitlePlans = make(map[string]state.PendingTitlePlan)
	committed.Generation++
	if err := s.Store.SaveState(committed); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_write_failed"}, err
	}
	return result, nil
}

func (s Service) report() (output.Result, error) {
	if s.Reports == nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "report_unavailable"}, errors.New("report input unavailable")
	}
	decoder := json.NewDecoder(s.Reports)
	decoder.DisallowUnknownFields()
	var envelope reportEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, err
	}
	if envelope.Reports == nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("reports must be an array")
	}
	for _, report := range envelope.Reports {
		if report.NativeSuccess == nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("native_success is required")
		}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("report must contain one JSON value")
	}
	lock, err := s.Store.AcquireLock()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "title_plan_locked"}, err
	}
	defer lock.Close()
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "config_read_failed"}, err
	}
	if !cfg.RenameEnabled {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "rename_disabled"}, errors.New("rename_disabled")
	}
	if err := refuseCycle(s.Store); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "cycle_in_progress"}, err
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_read_failed"}, err
	}
	result := output.TitleReportResult{AcceptedIDs: []string{}, RejectedIDs: []string{}}
	now := s.Now().UTC()
	counts := make(map[string]int, len(envelope.Reports))
	for _, report := range envelope.Reports {
		counts[report.TaskID]++
	}
	seenRejected := make(map[string]struct{}, len(envelope.Reports))
	changed := false
	for _, report := range envelope.Reports {
		plan, ok := committed.PendingTitlePlans[report.TaskID]
		validID := report.TaskID != "" && strings.TrimSpace(report.TaskID) == report.TaskID
		succeeded := *report.NativeSuccess
		validOutcome := succeeded != (report.ErrorCode != "")
		if counts[report.TaskID] != 1 || !validID || !ok || plan.OperationID != report.OperationID || !validOutcome {
			if validID {
				if _, seen := seenRejected[report.TaskID]; !seen {
					result.RejectedIDs = append(result.RejectedIDs, report.TaskID)
					seenRejected[report.TaskID] = struct{}{}
				}
			}
			continue
		}
		if plan.NativeOutcome == state.NativeTitleSucceeded {
			if succeeded {
				result.AcceptedIDs = append(result.AcceptedIDs, report.TaskID)
			} else {
				result.RejectedIDs = append(result.RejectedIDs, report.TaskID)
			}
			continue
		}
		if plan.NativeOutcome == state.NativeTitleFailed && !succeeded && plan.NativeErrorCode == report.ErrorCode {
			result.AcceptedIDs = append(result.AcceptedIDs, report.TaskID)
			continue
		}
		candidate := plan
		candidate.NativeReportedAt = &now
		if succeeded {
			candidate.NativeOutcome, candidate.NativeErrorCode = state.NativeTitleSucceeded, ""
		} else {
			candidate.NativeOutcome, candidate.NativeErrorCode = state.NativeTitleFailed, report.ErrorCode
		}
		if err := candidate.Validate(); err != nil {
			result.RejectedIDs = append(result.RejectedIDs, report.TaskID)
			continue
		}
		committed.PendingTitlePlans[report.TaskID] = candidate
		result.AcceptedIDs = append(result.AcceptedIDs, report.TaskID)
		changed = true
	}
	sort.Strings(result.AcceptedIDs)
	sort.Strings(result.RejectedIDs)
	if changed {
		committed.Generation++
		if err := s.Store.SaveState(committed); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_write_failed"}, err
		}
	}
	return result, nil
}

func applyCanonical(committed *state.State, plan state.PendingTitlePlan, task codex.Task) {
	if record, ok := committed.Tasks[plan.TaskID]; ok {
		if task.Revision == plan.ExpectedRevision {
			record.CapturedRevision = task.Revision
		}
		record.CapturedTitle, record.LastAppliedTitle = task.Title, task.Title
		record.DurableSubject, record.ManagedAction = plan.DurableSubject, plan.ManagedAction
		record.ManagedTokenDisplay, record.ManagedTokenPosition = plan.ManagedTokenDisplay, plan.ManagedTokenPosition
		committed.Tasks[plan.TaskID] = record
	}
	delete(committed.PendingTitlePlans, plan.TaskID)
}
