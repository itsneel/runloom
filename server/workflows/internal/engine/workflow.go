package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/runloom/workflows/internal/dbnode"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type interpreter struct {
	trace        *executionTrace
	handlerRoot  bool
	handlerProc  *signalHandlers
	scope        string
	budgetPauses map[string]model.PauseGate
	ended        bool
	endOutput    any
	input        model.RunInput
	state        *model.RunState
	approvals    map[string]model.ApprovalDecision
	callbacks    map[string]model.WorkflowEvent
	resumes      map[string]model.WorkflowEvent
	signals      map[string]model.WorkflowEvent
}

type branchOutcome struct {
	Index int
	ID    string
	Steps map[string]any
	Err   error
}

func RunloomWorkflow(ctx workflow.Context, input model.RunInput) (result model.RunResult, runErr error) {
	// Trust Temporal's parent identity, not caller-controlled source/parent fields.
	// Legacy inputs without kind keep their original command sequence on replay.
	if input.Definition.Kind == "child" && workflow.GetInfo(ctx).ParentWorkflowExecution == nil {
		return result, temporal.NewNonRetryableApplicationError("child workflows can only be started by a parent workflow", "ChildOnlyWorkflow", nil)
	}
	return runWorkflow(ctx, input)
}

// A separate entry point for authenticated Studio tests. The normal workflow
// type keeps its parent-only guard even if source or is_test fields are forged.
func RunloomChildTestWorkflow(ctx workflow.Context, input model.RunInput) (model.RunResult, error) {
	if input.Definition.Kind != "child" || input.Definition.Version < 1 {
		return model.RunResult{}, temporal.NewNonRetryableApplicationError("tests require a published child workflow", "InvalidChildTest", nil)
	}
	input.IsTest, input.Source = true, "child_test"
	input.ParentWorkflowID, input.ScheduleID = "", ""
	return runWorkflow(ctx, input)
}

// Draft tests have a separate type; existing production and published child
// histories retain their entry-point guards and command sequences.
func RunloomDraftTestWorkflow(ctx workflow.Context, input model.RunInput) (model.RunResult, error) {
	if input.Definition.Version != 0 || (input.Definition.Kind != "main" && input.Definition.Kind != "child") {
		return model.RunResult{}, temporal.NewNonRetryableApplicationError("tests require a draft workflow snapshot", "InvalidDraftTest", nil)
	}
	input.IsTest, input.Source = true, "draft_test"
	input.ParentWorkflowID, input.ScheduleID = "", ""
	return runWorkflow(ctx, input)
}

func runWorkflow(ctx workflow.Context, input model.RunInput) (result model.RunResult, runErr error) {
	if input.Metadata != nil {
		m := *input.Metadata
		info := workflow.GetInfo(ctx)
		// Namespace routing is enforced outside workflow code. Offline history
		// replay uses ReplayNamespace, not the original namespace.
		if m.TeamID != input.TeamID || m.OrgID == "" {
			return result, temporal.NewNonRetryableApplicationError("execution namespace identity mismatch", "InvalidNamespace", nil)
		}
		if input.ParentWorkflowID == "" {
			m.RootExecutionID = info.WorkflowExecution.ID
		}
		m.IsTest, m.Source = input.IsTest, input.Source
		input.Metadata = &m
		if err := workflow.UpsertTypedSearchAttributes(ctx, m.SearchUpdates()...); err != nil {
			return result, err
		}
	}
	state := &model.RunState{
		Metadata:          input.Metadata,
		IsTest:            input.IsTest,
		ChildWorkflows:    map[string]string{},
		ParentWorkflowID:  input.ParentWorkflowID,
		ScheduleID:        input.ScheduleID,
		Status:            "running",
		DefinitionID:      input.Definition.ID,
		DefinitionVersion: input.Definition.Version,
		CompletedSteps:    []string{},
		WaitingApprovals:  map[string]model.ApprovalGate{},
		WaitingCallbacks:  map[string]model.CallbackGate{},
		PausedSteps:       map[string]model.PauseGate{},
		WaitingSignals:    map[string]model.SignalGate{},
		Steps:             map[string]model.StepState{},
		StartedAt:         workflow.Now(ctx).Format(time.RFC3339Nano),
	}
	budgetPauses := map[string]model.PauseGate{}
	if err := workflow.SetQueryHandler(ctx, "status", func() (model.RunState, error) {
		snapshot := *state
		if len(state.PausedSteps) > 0 && state.Status != "cancelled" && state.Status != "failed" && state.Status != "completed" {
			ids := make([]string, 0, len(state.PausedSteps))
			//workflowcheck:ignore read-only query; sorted before selecting a gate
			for id := range state.PausedSteps {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			gate := state.PausedSteps[ids[0]]
			snapshot.Status, snapshot.PausedStep = "paused", &gate
		}
		if len(budgetPauses) > 0 && state.Status != "cancelled" && state.Status != "failed" {
			snapshot.Status = "paused"
			ids := make([]string, 0, len(budgetPauses))
			//workflowcheck:ignore query emits no commands; keys are sorted before choosing a gate
			for id := range budgetPauses {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			gate := budgetPauses[ids[0]]
			snapshot.PausedStep = &gate
		}
		return snapshot, nil
	}); err != nil {
		return result, err
	}
	trace := &executionTrace{ExecutionTrace: model.ExecutionTrace{Nodes: []*model.NodeExecution{}}}
	if err := workflow.SetQueryHandler(ctx, "execution_trace", func() (model.ExecutionTrace, error) { return trace.ExecutionTrace, nil }); err != nil {
		return result, err
	}
	runner := &interpreter{
		trace:        trace,
		budgetPauses: budgetPauses,
		input:        input,
		state:        state,
		approvals:    map[string]model.ApprovalDecision{},
		callbacks:    map[string]model.WorkflowEvent{},
		resumes:      map[string]model.WorkflowEvent{},
		signals:      map[string]model.WorkflowEvent{},
	}
	mainCtx := ctx
	var cancelHandlers workflow.CancelFunc
	if len(input.Definition.SignalHandlers) > 0 {
		var cancelMain workflow.CancelFunc
		mainCtx, cancelMain = workflow.WithCancel(ctx)
		handlerCtx, cancel := workflow.WithCancel(ctx)
		cancelHandlers = cancel
		defer cancelHandlers()
		runner.handlerProc = newSignalHandlers(handlerCtx, runner, cancelMain)
		defer func() {
			//workflowcheck:ignore lifecycle status changes issue no commands
			for name, state := range runner.state.SignalHandlers {
				state.Accepting = false
				if state.Status != "failed" && state.Status != "closed" {
					state.Status = "cancelled"
				}
				runner.state.SignalHandlers[name] = state
			}
		}()
	}
	approvalChannel := workflow.GetSignalChannel(ctx, "approval")
	workflow.Go(ctx, func(ctx workflow.Context) {
		for {
			var decision model.ApprovalDecision
			if more := approvalChannel.Receive(ctx, &decision); !more {
				return
			}
			if decision.StepID != "" {
				runner.approvals[decision.StepID] = decision
			}
		}
	})
	signalChannel := workflow.GetSignalChannel(ctx, "signal")
	workflow.Go(ctx, func(ctx workflow.Context) {
		for {
			var event model.WorkflowEvent
			if more := signalChannel.Receive(ctx, &event); !more {
				return
			}
			if event.SignalName != "" {
				if runner.handlerProc != nil && runner.handlerProc.enqueue(event) {
					continue
				}
				runner.signals[event.SignalName] = event
			}
		}
	})
	callbackChannel := workflow.GetSignalChannel(ctx, "callback")
	workflow.Go(ctx, func(ctx workflow.Context) {
		for {
			var event model.WorkflowEvent
			if more := callbackChannel.Receive(ctx, &event); !more {
				return
			}
			if event.StepID != "" {
				runner.callbacks[event.StepID] = event
			}
		}
	})
	resumeChannel := workflow.GetSignalChannel(ctx, "resume")
	workflow.Go(ctx, func(ctx workflow.Context) {
		for {
			var event model.WorkflowEvent
			if more := resumeChannel.Receive(ctx, &event); !more {
				return
			}
			if event.StepID != "" {
				runner.resumes[event.StepID] = event
			}
		}
	})

	defer func() {
		if runErr == nil {
			return
		}
		message := runErr.Error()
		state.Error = &message
		if errors.Is(runErr, workflow.ErrCanceled) {
			state.Status = "cancelled"
		} else {
			state.Status = "failed"
		}
	}()

	// New scheduled/child inputs opt into registration. Old/manual histories
	// do not emit an extra activity command and remain replay compatible.
	if input.Source == "schedule" || input.Source == "child" {
		registration := input
		registration.Definitions = nil
		registration.Definition.Steps = nil
		registration.Definition.SignalHandlers = nil
		registration.Definition.Output = nil
		activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, ScheduleToCloseTimeout: 5 * time.Minute})
		if err := workflow.ExecuteActivity(activityCtx, "runloom.internal.record_run", registration).Get(activityCtx, nil); err != nil {
			return result, err
		}
	}
	context := definition.Context{Input: input.Input, Steps: map[string]any{}}
	if runner.handlerProc != nil {
		if err := workflow.Await(ctx, func() bool { return runner.handlerProc.ready }); err != nil {
			return result, err
		}
	}
	if err := runner.executeSteps(mainCtx, input.Definition.Steps, &context, true); err != nil {
		if runner.handlerProc != nil && runner.handlerProc.failure != nil {
			return result, runner.handlerProc.failure
		}
		return result, err
	}
	if runner.handlerProc != nil {
		if err := runner.handlerProc.finish(ctx, signalChannel); err != nil {
			return result, err
		}
	}
	output := input.Definition.Output
	if runner.ended {
		output = runner.endOutput
	} else if output == nil {
		output = context.Steps
	} else {
		var err error
		output, err = definition.Resolve(output, context.Value())
		if err != nil {
			return result, err
		}
	}
	state.Status = "completed"
	state.CurrentStep = nil
	state.WaitingApproval = nil
	state.WaitingCallback = nil
	state.PausedStep = nil
	state.WaitingSignal = nil
	state.Output = output
	state.CompletedAt = workflow.Now(ctx).Format(time.RFC3339Nano)
	stepOutputs := make(map[string]model.StepOutput, len(context.Steps))
	stepKeys := make([]string, 0, len(context.Steps))
	//workflowcheck:ignore map keys are sorted before producing the result
	for key := range context.Steps {
		stepKeys = append(stepKeys, key)
	}
	sort.Strings(stepKeys)
	for _, key := range stepKeys {
		raw := context.Steps[key]
		value := raw.(map[string]any)
		stepOutputs[key] = model.StepOutput{Output: value["output"]}
	}
	if runner.handlerProc != nil {
		//workflowcheck:ignore result assembly emits no commands
		for key, value := range state.Steps {
			stepOutputs[key] = model.StepOutput{Output: value.Output}
		}
	}
	return model.RunResult{Status: "completed", Output: output, Steps: stepOutputs, SignalHandlers: state.SignalHandlers}, nil
}

func (r *interpreter) executeSteps(ctx workflow.Context, steps []model.Step, values *definition.Context, trackState bool) error {
	for _, step := range steps {
		// A budget pause prevents new steps in sibling branches. Already dispatched
		// activities are not revoked; their external side effects cannot be undone.
		if err := workflow.Await(ctx, func() bool { return len(r.budgetPauses) == 0 }); err != nil {
			return err
		}
		if r.ended {
			break
		}
		if trackState {
			name := step.Name
			if name == "" {
				name = step.ID
			}
			r.state.CurrentStep = &model.CurrentStep{ID: step.ID, Name: name, Type: step.Type}
		}
		executionStep := step
		if r.scope != "" {
			executionStep.ID = scopedStepID(r.scope, step.ID)
			if step.Type == "signal" {
				executionStep.SignalName = scopedStepID(r.scope, step.SignalName)
			}
		}
		nodeCtx, node := r.beginNode(ctx, step)
		output, err := r.executeStep(nodeCtx, executionStep, values, trackState)
		if err != nil {
			r.finishNode(ctx, node, nil, err)
			if step.Type == "decision" {
				state := r.state.Steps[executionStep.ID]
				state.Status = "failed"
				if temporal.IsCanceledError(err) {
					state.Status = "cancelled"
				}
				r.state.Steps[executionStep.ID] = state
			}
			return err
		}
		if step.OutputSchema != nil {
			validationCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 15 * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
			if err := workflow.ExecuteActivity(validationCtx, "runloom.internal.validate_output", map[string]any{"schema": step.OutputSchema, "output": output}).Get(validationCtx, nil); err != nil {
				r.finishNode(ctx, node, nil, err)
				return err
			}
		}
		r.finishNode(ctx, node, output, nil)
		values.Steps[step.ID] = map[string]any{"output": output}
		if trackState || r.scope != "" {
			r.state.Steps[executionStep.ID] = model.StepState{Status: "completed", Output: output}
			r.state.CompletedSteps = append(r.state.CompletedSteps, executionStep.ID)
		}
	}
	return nil
}

func (r *interpreter) executeStep(ctx workflow.Context, step model.Step, values *definition.Context, trackState bool) (any, error) {
	switch step.Type {
	case "integration":
		if step.Integration == nil {
			return nil, fmt.Errorf("integration configuration is required")
		}
		in := *step.Integration
		raw, err := definition.Resolve(in.Parameters, values.Value())
		if err != nil {
			return nil, err
		}
		r.recordInput(ctx, raw)
		in.Parameters, _ = raw.(map[string]any)
		activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 60 * time.Second, ScheduleToCloseTimeout: 2 * time.Minute, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
		var output map[string]any
		err = workflow.ExecuteActivity(activityCtx, "runloom.internal.integration", model.IntegrationInput{TeamID: r.input.TeamID, Integration: in}).Get(activityCtx, &output)
		return output, err
	case "transform", "collection", "merge":
		if err := definition.ValidateData(step.Data, step.Type); err != nil {
			return nil, temporal.NewNonRetryableApplicationError(err.Error(), "InvalidDataOperation", nil)
		}
		source, sourceErr := definition.Resolve(step.Data.Source, values.Value())
		if sourceErr == nil {
			if step.Data.Other == nil {
				r.recordInput(ctx, source)
			} else if other, err := definition.Resolve(step.Data.Other, values.Value()); err == nil {
				r.recordInput(ctx, map[string]any{"source": source, "other": other})
			}
		}
		output, err := definition.RunData(step.Data, values.Value())
		if err != nil {
			return nil, temporal.NewNonRetryableApplicationError(err.Error(), "InvalidDataOperation", nil)
		}
		return output, nil
	case "foreach":
		return r.executeForEach(ctx, step, values)
	case "fail":
		message, err := definition.Resolve(step.Message, values.Value())
		if err != nil {
			return nil, err
		}
		r.recordInput(ctx, message)
		text, ok := message.(string)
		if !ok || text == "" || len(text) > 2000 {
			text = "Workflow explicitly failed"
		}
		r.state.Steps[step.ID] = model.StepState{Status: "failed"}
		return nil, temporal.NewNonRetryableApplicationError(text, "WorkflowFailed", nil)
	case "database":
		if step.Database == nil {
			return nil, fmt.Errorf("database configuration is required")
		}
		op, err := definition.ResolveDatabase(*step.Database, values.Value())
		if err != nil {
			return nil, err
		}
		r.recordInput(ctx, op)
		activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 25 * time.Second, ScheduleToCloseTimeout: time.Minute, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
		var output any
		err = workflow.ExecuteActivity(activityCtx, "runloom.internal.database", dbnode.Input{TeamID: r.input.TeamID, Operation: op}).Get(activityCtx, &output)
		return output, err
	case "decision":
		return r.executeDecision(ctx, step, values)
	case "end":
		if !trackState && !r.handlerRoot {
			return nil, temporal.NewNonRetryableApplicationError("End is not allowed in parallel branches", "InvalidDefinition", nil)
		}
		output, err := definition.Resolve(step.With, values.Value())
		if err != nil {
			return nil, err
		}
		r.recordInput(ctx, output)
		r.ended, r.endOutput = true, output
		return map[string]any{"ended": true, "message": step.Message}, nil
	case "email":
		if step.Email == nil {
			return nil, fmt.Errorf("email configuration is required")
		}
		message, err := definition.ResolveEmail(*step.Email, values.Value())
		if err != nil {
			return nil, err
		}
		r.recordInput(ctx, message)
		activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second, ScheduleToCloseTimeout: 2 * time.Minute,
			RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		var output any
		err = workflow.ExecuteActivity(activityCtx, "runloom.internal.email", model.EmailActivityInput{TeamID: r.input.TeamID, Message: message}).Get(activityCtx, &output)
		return output, err
	case "code":
		if step.Code == nil {
			return nil, fmt.Errorf("code configuration is required")
		}
		resolved, err := definition.Resolve(step.With, values.Value())
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			resolved = map[string]any{}
		}
		payload, ok := resolved.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("code input must resolve to an object")
		}
		r.recordInput(ctx, payload)
		activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 8 * time.Second, ScheduleToCloseTimeout: 60 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 5, InitialInterval: time.Second, MaximumInterval: 10 * time.Second},
		})
		var output any
		err = workflow.ExecuteActivity(activityCtx, "runloom.internal.code", model.CodeActivityInput{TeamID: r.input.TeamID, Code: *step.Code, Input: payload}).Get(activityCtx, &output)
		return output, err
	case "http":
		request := *step.HTTP
		url, err := definition.Resolve(request.URL, values.Value())
		if err != nil {
			return nil, err
		}
		var ok bool
		request.URL, ok = url.(string)
		if !ok {
			return nil, temporal.NewNonRetryableApplicationError("API URL must resolve to text", "InvalidDefinition", nil)
		}
		request.Body, err = definition.Resolve(request.Body, values.Value())
		if err != nil {
			return nil, err
		}
		r.recordInput(ctx, map[string]any{"method": request.Method, "url": request.URL, "body": request.Body})
		retry := step.Retry
		activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Duration(*step.TimeoutSeconds) * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: int32(retry.MaximumAttempts), InitialInterval: time.Duration(retry.InitialInterval * float64(time.Second)), BackoffCoefficient: retry.BackoffCoefficient, MaximumInterval: time.Duration(retry.MaximumInterval * float64(time.Second))}})
		var output any
		err = workflow.ExecuteActivity(activityCtx, "runloom.internal.http", model.HTTPActivityInput{TeamID: r.input.TeamID, Request: request}).Get(activityCtx, &output)
		return output, err
	case "subworkflow":
		child, ok := r.input.Definitions[definition.DefinitionKey(step.DefinitionID, step.Version)]
		if !ok {
			return nil, fmt.Errorf("missing pinned subworkflow %s@%d", step.DefinitionID, step.Version)
		}
		resolved, err := definition.Resolve(step.With, values.Value())
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			resolved = map[string]any{}
		}
		payload, ok := resolved.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("subworkflow input must be an object")
		}
		r.recordInput(ctx, payload)
		info := workflow.GetInfo(ctx)
		childID := fmt.Sprintf("%s-child-%s-%s", r.input.TeamID, info.WorkflowExecution.RunID, step.ID)
		var childMetadata *model.ExecutionMetadata
		if r.input.Metadata != nil {
			m, ok := r.input.ChildMetadata[definition.DefinitionKey(step.DefinitionID, step.Version)]
			if !ok {
				return nil, fmt.Errorf("missing child execution metadata")
			}
			m.RootExecutionID, m.Source, m.IsTest = r.input.Metadata.RootExecutionID, "child", r.input.IsTest
			childMetadata = &m
		}
		r.state.ChildWorkflows[step.ID] = childID
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			TypedSearchAttributes: childMetadata.SearchAttributes(),
			WorkflowID:            childID, WorkflowExecutionTimeout: time.Duration(*step.TimeoutSeconds) * time.Second,
			ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_TERMINATE, WaitForCancellation: true,
			RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		var result model.RunResult
		childInput := model.RunInput{
			Metadata: childMetadata, ChildMetadata: r.input.ChildMetadata,
			IsTest:     r.input.IsTest,
			Definition: child, Definitions: r.input.Definitions, Input: payload,
			TeamID: r.input.TeamID, Source: "child", Actor: r.input.Actor, ParentWorkflowID: info.WorkflowExecution.ID,
		}
		err = workflow.ExecuteChildWorkflow(childCtx, model.WorkflowType("RunloomWorkflow", childInput), childInput).Get(childCtx, &result)
		if err != nil {
			return nil, err
		}
		return result.Output, nil
	case "activity":
		if strings.HasPrefix(step.Activity, "runloom.internal.") {
			return nil, fmt.Errorf("internal runtime activities are not callable by action nodes")
		}
		payload, err := definition.Resolve(step.With, values.Value())
		if err != nil {
			return nil, err
		}
		if payload == nil {
			payload = map[string]any{}
		}
		r.recordInput(ctx, payload)
		if step.Activity == "runloom.llm" && workflow.GetVersion(ctx, "llm-usage-metadata-v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
			if fields, ok := payload.(map[string]any); ok {
				fields["_runloom_step_id"] = step.ID
			}
		}
		retry := step.Retry
		activityContext := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: time.Duration(*step.TimeoutSeconds) * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts:    int32(retry.MaximumAttempts),
				InitialInterval:    time.Duration(retry.InitialInterval * float64(time.Second)),
				BackoffCoefficient: retry.BackoffCoefficient,
				MaximumInterval:    time.Duration(retry.MaximumInterval * float64(time.Second)),
			},
		})
		var output any
		if step.Activity == "runloom.llm" {
			return r.executeBudgetedLLM(activityContext, step, payload)
		}
		if err := workflow.ExecuteActivity(activityContext, step.Activity, payload).Get(activityContext, &output); err != nil {
			return nil, err
		}
		return output, nil
	case "llm":
		payload, err := definition.Resolve(map[string]any{
			"model":         step.Model,
			"system_prompt": step.SystemPrompt,
			"prompt":        step.Prompt,
			"temperature":   *step.Temperature,
			"max_tokens":    *step.MaxTokens,
		}, values.Value())
		if err != nil {
			return nil, err
		}
		if workflow.GetVersion(ctx, "llm-usage-metadata-v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
			payload.(map[string]any)["_runloom_step_id"] = step.ID
		}
		if err := promptPayload(step, values, payload.(map[string]any)); err != nil {
			return nil, err
		}
		r.recordInput(ctx, payload)
		if step.LLMTask != nil {
			payload.(map[string]any)["_runloom_llm_task"] = step.LLMTask
		}
		retry := step.Retry
		activityContext := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: time.Duration(*step.TimeoutSeconds) * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts:    int32(retry.MaximumAttempts),
				InitialInterval:    time.Duration(retry.InitialInterval * float64(time.Second)),
				BackoffCoefficient: retry.BackoffCoefficient,
				MaximumInterval:    time.Duration(retry.MaximumInterval * float64(time.Second)),
			},
		})
		return r.executeBudgetedLLM(activityContext, step, payload)
	case "wait":
		r.recordInput(ctx, map[string]any{"seconds": step.Seconds})
		if err := workflow.Sleep(ctx, time.Duration(step.Seconds)*time.Second); err != nil {
			return nil, err
		}
		return map[string]any{"waited_seconds": step.Seconds}, nil
	case "approval":
		return r.waitForApproval(ctx, step, values, trackState)
	case "callback":
		return r.waitForCallback(ctx, step, values, trackState)
	case "pause":
		return r.waitForResume(ctx, step, values, trackState)
	case "signal":
		return r.waitForSignal(ctx, step, values, trackState)
	case "condition":
		if step.If != nil {
			left, leftErr := definition.Resolve(step.If.Left, values.Value())
			right, rightErr := definition.Resolve(step.If.Right, values.Value())
			if leftErr == nil && rightErr == nil {
				r.recordInput(ctx, map[string]any{"left": left, "operator": step.If.Operator, "right": right})
			}
		}
		matched, err := definition.Evaluate(step.If, values.Value())
		if err != nil {
			return nil, err
		}
		selected := step.Else
		branch := "else"
		if matched {
			selected = step.Then
			branch = "then"
		}
		if err := r.executeSteps(ctx, selected, values, trackState); err != nil {
			return nil, err
		}
		return map[string]any{"matched": matched, "branch": branch}, nil
	case "parallel":
		ids := make([]string, 0, len(step.Branches))
		for _, branch := range step.Branches {
			ids = append(ids, branch.ID)
		}
		r.recordInput(ctx, map[string]any{"branches": ids})
		return r.executeParallel(ctx, step, values)
	default:
		return nil, temporal.NewNonRetryableApplicationError("unsupported step type", "InvalidDefinition", nil)
	}
}

func (r *interpreter) waitForCallback(ctx workflow.Context, step model.Step, values *definition.Context, trackState bool) (any, error) {
	prompt, err := definition.Resolve(step.Prompt, values.Value())
	if err != nil {
		return nil, err
	}
	promptText, ok := prompt.(string)
	if !ok {
		return nil, fmt.Errorf("callback prompt must resolve to text")
	}
	gate := model.CallbackGate{StepID: step.ID, CallbackKey: step.CallbackKey, Prompt: promptText}
	r.recordInput(ctx, gate)
	r.state.WaitingCallbacks[step.ID] = gate
	if trackState {
		r.state.Status = "waiting_callback"
		r.state.WaitingCallback = &gate
	}
	defer func() {
		delete(r.state.WaitingCallbacks, step.ID)
		if trackState {
			r.state.Status = "running"
			r.state.WaitingCallback = nil
		}
	}()
	condition := func() bool { _, present := r.callbacks[step.ID]; return present }
	if step.TimeoutSeconds == nil {
		err = workflow.Await(ctx, condition)
	} else {
		var met bool
		met, err = workflow.AwaitWithTimeout(ctx, time.Duration(*step.TimeoutSeconds)*time.Second, condition)
		if err == nil && !met {
			if step.OnTimeout == "fail" {
				return nil, temporal.NewNonRetryableApplicationError("callback timed out: "+step.ID, "CallbackTimeout", nil)
			}
			return map[string]any{"payload": nil, "actor": "system", "timed_out": true}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	event := r.callbacks[step.ID]
	delete(r.callbacks, step.ID)
	return map[string]any{"payload": event.Payload, "actor": event.Actor}, nil
}

func (r *interpreter) waitForResume(ctx workflow.Context, step model.Step, values *definition.Context, trackState bool) (any, error) {
	message, err := definition.Resolve(step.Message, values.Value())
	if err != nil {
		return nil, err
	}
	messageText, ok := message.(string)
	if !ok {
		return nil, fmt.Errorf("pause message must resolve to text")
	}
	gate := model.PauseGate{StepID: step.ID, Message: messageText}
	r.recordInput(ctx, gate)
	r.state.PausedSteps[step.ID] = gate
	if trackState {
		r.state.Status = "paused"
		r.state.PausedStep = &gate
	}
	defer func() {
		delete(r.state.PausedSteps, step.ID)
		if trackState {
			r.state.Status = "running"
			r.state.PausedStep = nil
		}
	}()
	condition := func() bool { _, present := r.resumes[step.ID]; return present }
	if step.TimeoutSeconds == nil {
		err = workflow.Await(ctx, condition)
	} else {
		var met bool
		met, err = workflow.AwaitWithTimeout(ctx, time.Duration(*step.TimeoutSeconds)*time.Second, condition)
		if err == nil && !met {
			if step.OnTimeout == "fail" {
				return nil, temporal.NewNonRetryableApplicationError("pause timed out: "+step.ID, "PauseTimeout", nil)
			}
			return map[string]any{"payload": nil, "actor": "system", "timed_out": true}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	event := r.resumes[step.ID]
	delete(r.resumes, step.ID)
	return map[string]any{"payload": event.Payload, "actor": event.Actor}, nil
}

func (r *interpreter) waitForSignal(ctx workflow.Context, step model.Step, values *definition.Context, trackState bool) (any, error) {
	prompt, err := definition.Resolve(step.Prompt, values.Value())
	if err != nil {
		return nil, err
	}
	promptText, ok := prompt.(string)
	if !ok {
		return nil, fmt.Errorf("signal prompt must resolve to text")
	}
	gate := model.SignalGate{StepID: step.ID, SignalName: step.SignalName, Prompt: promptText}
	r.recordInput(ctx, gate)
	r.state.WaitingSignals[step.SignalName] = gate
	if trackState {
		r.state.Status = "waiting_signal"
		r.state.WaitingSignal = &gate
	}
	defer func() {
		delete(r.state.WaitingSignals, step.SignalName)
		if trackState {
			r.state.Status = "running"
			r.state.WaitingSignal = nil
		}
	}()
	condition := func() bool { _, present := r.signals[step.SignalName]; return present }
	if step.TimeoutSeconds == nil {
		err = workflow.Await(ctx, condition)
	} else {
		var met bool
		met, err = workflow.AwaitWithTimeout(ctx, time.Duration(*step.TimeoutSeconds)*time.Second, condition)
		if err == nil && !met {
			if step.OnTimeout == "fail" {
				return nil, temporal.NewNonRetryableApplicationError("signal timed out: "+step.SignalName, "SignalTimeout", nil)
			}
			return map[string]any{"payload": nil, "actor": "system", "signal_name": step.SignalName, "timed_out": true}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	event := r.signals[step.SignalName]
	delete(r.signals, step.SignalName)
	return map[string]any{"payload": event.Payload, "actor": event.Actor, "signal_name": step.SignalName}, nil
}

func (r *interpreter) waitForApproval(ctx workflow.Context, step model.Step, values *definition.Context, trackState bool) (any, error) {
	prompt, err := definition.Resolve(step.Prompt, values.Value())
	if err != nil {
		return nil, err
	}
	promptText, ok := prompt.(string)
	if !ok {
		return nil, fmt.Errorf("approval prompt must resolve to text")
	}
	gate := model.ApprovalGate{StepID: step.ID, Prompt: promptText}
	r.recordInput(ctx, gate)
	r.state.WaitingApprovals[step.ID] = gate
	if trackState {
		r.state.WaitingApproval = &gate
	}
	defer func() {
		delete(r.state.WaitingApprovals, step.ID)
		if trackState {
			r.state.WaitingApproval = nil
		}
	}()
	condition := func() bool {
		_, present := r.approvals[step.ID]
		return present
	}
	if step.TimeoutSeconds == nil {
		err = workflow.Await(ctx, condition)
	} else {
		var met bool
		met, err = workflow.AwaitWithTimeout(ctx, time.Duration(*step.TimeoutSeconds)*time.Second, condition)
		if err == nil && !met {
			if step.OnTimeout == "fail" {
				return nil, temporal.NewNonRetryableApplicationError("approval timed out: "+step.ID, "ApprovalTimeout", nil)
			}
			return map[string]any{"approved": false, "comment": "approval timed out", "actor": "system"}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	decision := r.approvals[step.ID]
	return map[string]any{"approved": decision.Approved, "comment": decision.Comment, "actor": decision.Actor}, nil
}

func (r *interpreter) executeParallel(ctx workflow.Context, step model.Step, values *definition.Context) (any, error) {
	branchContext, cancel := workflow.WithCancel(ctx)
	defer cancel()
	results := workflow.NewBufferedChannel(ctx, len(step.Branches))
	for index, branch := range step.Branches {
		index, branch := index, branch
		workflow.Go(branchContext, func(branchCtx workflow.Context) {
			localSteps := make(map[string]any, len(values.Steps))
			//workflowcheck:ignore map copy does not issue workflow commands and lookup order is irrelevant
			for key, value := range values.Steps {
				localSteps[key] = value
			}
			local := definition.Context{Input: values.Input, Signal: values.Signal, Steps: localSteps, Decision: values.Decision, Loop: values.Loop}
			branchRunner := *r
			branchRunner.handlerRoot = false
			err := branchRunner.executeSteps(branchCtx, branch.Steps, &local, false)
			additions := map[string]any{}
			//workflowcheck:ignore additions are sorted before they are merged
			for key, value := range local.Steps {
				if _, existed := values.Steps[key]; !existed {
					additions[key] = value
				}
			}
			results.Send(ctx, branchOutcome{Index: index, ID: branch.ID, Steps: additions, Err: err})
		})
	}
	outcomes := make([]branchOutcome, 0, len(step.Branches))
	for range step.Branches {
		var outcome branchOutcome
		results.Receive(ctx, &outcome)
		outcomes = append(outcomes, outcome)
		if outcome.Err != nil {
			cancel()
		}
	}
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].Index < outcomes[j].Index })
	summary := map[string]any{}
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			return nil, outcome.Err
		}
		keys := make([]string, 0, len(outcome.Steps))
		//workflowcheck:ignore map keys are sorted before they affect workflow state
		for key := range outcome.Steps {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			values.Steps[key] = outcome.Steps[key]
		}
		summary[outcome.ID] = map[string]any{"steps": keys}
	}
	return summary, nil
}
