// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localworkflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Stop persists stop intent before requesting cancellation of owned operations.
func (c *Coordinator) Stop(ctx context.Context, id domain.SessionID, version int64) (Snapshot, error) {
	current, e := c.Get(ctx, id)
	if e != nil {
		return Snapshot{}, e
	}
	if current.Run == nil {
		return Snapshot{}, domain.ErrLocalWorkflowNotFound
	}
	unlock := c.lock(current.Run.ID)
	defer unlock()
	r, _, e := c.deps.Store.GetLocalWorkflow(ctx, current.Run.ID)
	if e != nil {
		return Snapshot{}, e
	}
	if r.Version != version {
		return Snapshot{}, domain.ErrLocalWorkflowConflict
	}
	if r.Phase == domain.WorkflowStopped {
		return c.snapshot(ctx, r)
	}
	releaseAdmission, e := c.deps.Store.AcquireLocalReviewWorkspace(ctx, id)
	if e != nil {
		return Snapshot{}, e
	}
	r.StopRequested = true
	c.mu.Lock()
	for _, key := range []string{r.ID + ":plan", r.ID + ":provider-recovery"} {
		if cancel := c.active[key]; cancel != nil {
			cancel()
		}
	}
	c.mu.Unlock()
	stopProviderRetry(&r, "exhausted")
	r.Phase = domain.WorkflowStopping
	r.UpdatedAt = c.now()
	ok, e := c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, nil)
	// Publish the fence under admission, then release it before entering Chat
	// or review controllers, whose locks precede workspace admission.
	releaseAdmission()
	if e != nil {
		return Snapshot{}, e
	}
	if !ok {
		return Snapshot{}, domain.ErrLocalWorkflowConflict
	}
	r.Version++
	ops, e := c.deps.Store.ListLocalWorkflowOperations(ctx, r.ID)
	if e != nil {
		return Snapshot{}, e
	}
	c.mu.Lock()
	for _, op := range ops {
		if cancel := c.active[op.ID]; cancel != nil {
			cancel()
		}
	}
	c.mu.Unlock()
	c.closeGuard(r.ID)
	if e := c.finishStop(ctx, r, ops); e != nil {
		return Snapshot{}, e
	}
	return c.Get(ctx, id)
}
func activeOperation(op domain.LocalWorkflowOperation) bool {
	return op.State == "reserved" || op.State == "running" || op.State == "uncertain"
}
func (c *Coordinator) settleInterrupted(ctx context.Context, r domain.LocalWorkflowRun, op domain.LocalWorkflowOperation, results []domain.LocalWorkflowTestResult) {
	op.State = "interrupted"
	at := c.now()
	op.CompletedAt = &at
	if op.Kind == "tests" && c.testExecutionUncertain(ctx, op.ID) {
		op.State = "uncertain"
		op.CompletedAt = nil
		op.ErrorCode = "execution_uncertain"
		op.ErrorMessage = "Test host has no durable completion; previous commands may still run"
	}
	r.UpdatedAt = at
	if _, e := c.deps.Store.CompleteLocalWorkflowOperation(context.WithoutCancel(ctx), r.Version, op, r, results); e != nil {
		c.deps.Logger.Error("record interrupted workflow operation", "operation", op.ID, "error", e)
	}
}

// quiesce returns only after every previously reserved effect has a known end.
// A launch claim without a result is deliberately not evidence of absence.
func (c *Coordinator) quiesce(ctx context.Context, r domain.LocalWorkflowRun, ops []domain.LocalWorkflowOperation) (domain.LocalWorkflowRun, bool, error) {
	if r.BlockCode == "provider_execution_uncertain" || r.BlockCode == "provider_uncertain" || r.BlockCode == "review_uncertain" || c.isActive(r.ID+":plan") || c.isActive(r.ID+":provider-recovery") {
		return r, false, nil
	}
	if r.PlanState == "running" {
		err := c.block(ctx, r, "provider_execution_uncertain", fmt.Errorf("planner has no durable completion; end the affected session before starting another task"))
		return r, false, err
	}
	for _, op := range ops {
		if !activeOperation(op) {
			continue
		}
		if c.isActive(op.ID) {
			return r, false, nil
		}
		switch op.Kind {
		case "tests":
			state, e := c.deps.Tests.Reconcile(ctx, op.ID)
			if e != nil {
				return r, false, e
			}
			if state.State == "running" {
				if e := c.deps.Tests.Cancel(ctx, op.ID); e != nil {
					return r, false, e
				}
				return r, false, nil
			}
			if state.State == "uncertain" {
				return r, false, nil
			}
			c.settleInterrupted(ctx, r, op, state.Results)
		case "review":
			if op.State == "uncertain" || op.ReviewRunID == "" {
				err := c.block(ctx, r, "review_uncertain", fmt.Errorf("review launch has no durable completion; end the affected session before starting another task"))
				return r, false, err
			} // Failed status/interrupt is not exact process teardown.
			runID := op.ReviewRunID
			result, e := c.deps.Reviews.GetWorkflowLocal(ctx, runID)
			if e != nil {
				return r, false, e
			}
			if result.ID != "" && result.Status == domain.ReviewRunRunning {
				if e := c.deps.Reviews.CancelWorkflowLocal(ctx, result.ID); e != nil {
					return r, false, e
				}
				result, e = c.deps.Reviews.GetWorkflowLocal(ctx, result.ID)
				if e != nil {
					return r, false, e
				}
				if result.Status == domain.ReviewRunRunning {
					return r, false, nil
				}
			}
			c.settleInterrupted(ctx, r, op, nil)
		case "fix":
			obs, e := c.deps.Worker.Observe(ctx, r.SessionID, op.ID)
			if e != nil {
				return r, false, e
			}
			if obs.State == "running" || obs.State == "queued" || obs.State == "needs_input" {
				if e := c.deps.Worker.Interrupt(ctx, r.SessionID, op.ID); e != nil {
					return r, false, e
				}
				return r, false, nil
			}
			if obs.State == "uncertain" {
				return r, false, nil
			}
			c.settleInterrupted(ctx, r, op, nil)
		}
		var ok bool
		var e error
		r, ok, e = c.deps.Store.GetLocalWorkflow(ctx, r.ID)
		if e != nil || !ok {
			return r, false, e
		}
	}
	return r, true, nil
}
func (c *Coordinator) finishStop(ctx context.Context, r domain.LocalWorkflowRun, ops []domain.LocalWorkflowOperation) error {
	r, quiet, e := c.quiesce(ctx, r, ops)
	if e != nil {
		return e
	}
	if !quiet {
		return nil
	}
	obs, e := c.deps.Worker.Observe(ctx, r.SessionID, "")
	if e != nil {
		return e
	}
	if obs.State == "running" || obs.State == "queued" || obs.State == "needs_input" {
		return c.deps.Worker.Interrupt(ctx, r.SessionID, "")
	}
	if obs.State == "uncertain" {
		return nil
	}
	r.Phase = domain.WorkflowStopped
	r.UpdatedAt = c.now()
	_, e = c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, nil)
	return e
}

// Resume deliberately retries verification only after prior effects have settled.
func (c *Coordinator) Resume(ctx context.Context, id domain.SessionID, version int64, maxRounds *int) (Snapshot, error) {
	snap, e := c.Get(ctx, id)
	if e != nil {
		return Snapshot{}, e
	}
	if snap.Run == nil {
		return Snapshot{}, domain.ErrLocalWorkflowNotFound
	}
	unlock := c.lock(snap.Run.ID)
	defer unlock()
	r, _, e := c.deps.Store.GetLocalWorkflow(ctx, snap.Run.ID)
	if e != nil {
		return Snapshot{}, e
	}
	if r.Version != version {
		return Snapshot{}, domain.ErrLocalWorkflowConflict
	}
	if r.Phase != domain.WorkflowBlocked && r.Phase != domain.WorkflowStopped {
		return Snapshot{}, fmt.Errorf("%w: workflow must be stopped or blocked before resume", domain.ErrLocalWorkflowConflict)
	}
	if maxRounds != nil {
		if *maxRounds < r.MaxRounds || *maxRounds < 1 {
			return Snapshot{}, fmt.Errorf("%w: limit cannot be reduced", domain.ErrLocalWorkflowInvalid)
		}
		r.MaxRounds = *maxRounds
	}
	if r.BlockCode == "round_limit" && r.MaxRounds <= r.Cycle {
		return Snapshot{}, fmt.Errorf("%w: explicitly increase the round limit to continue", domain.ErrLocalWorkflowConflict)
	}
	ops, e := c.deps.Store.ListLocalWorkflowOperations(ctx, r.ID)
	if e != nil {
		return Snapshot{}, e
	}
	// Do not publish the new limit before quiescence checks have succeeded.
	limit := r.MaxRounds
	r, quiet, e := c.quiesce(ctx, r, ops)
	if e != nil {
		return Snapshot{}, e
	}
	if !quiet {
		return Snapshot{}, fmt.Errorf("%w: a previous operation is still active or uncertain; stop it before resuming", domain.ErrLocalWorkflowConflict)
	}
	r.MaxRounds = limit
	obs, e := c.deps.Worker.Observe(ctx, id, "")
	if e != nil {
		return Snapshot{}, e
	}
	if obs.State == "failed" || obs.State == "interrupted" {
		session, found, err := c.deps.Store.GetSession(ctx, id)
		if err != nil {
			return Snapshot{}, err
		}
		if !found || session.IsTerminated || session.Activity.State != domain.ActivityIdle || obs.TurnID == "" {
			return Snapshot{}, fmt.Errorf("%w: executor must be idle before resuming", domain.ErrLocalWorkflowConflict)
		}
		r.VerificationAfterTurnID = obs.TurnID
	} else if obs.State != "idle" && obs.State != "completed" {
		return Snapshot{}, fmt.Errorf("%w: executor is %s", domain.ErrLocalWorkflowConflict, obs.State)
	}
	if r.BlockCode == "configuration" || r.Automatic && (r.BlockCode == "verification_plan" || strings.HasPrefix(r.BlockCode, "provider_")) {
		session, ok, e := c.deps.Store.GetSession(ctx, id)
		if e != nil || !ok {
			return Snapshot{}, e
		}
		project, ok, e := c.deps.Store.GetProject(ctx, string(session.ProjectID))
		if e != nil || !ok {
			return Snapshot{}, e
		}
		cfg := project.Config.WithDefaults()
		if cfg.EffectiveReviewMode() != domain.ReviewTargetLocal || session.Harness != domain.HarnessCodex || len(cfg.TestCommands) == 0 && !r.Automatic || len(cfg.Reviewers) != 1 || cfg.Reviewers[0].Harness != domain.ReviewerClaudeCode || cfg.Validate() != nil {
			return Snapshot{}, fmt.Errorf("%w: fix Local review, Codex, Claude and test configuration first", domain.ErrLocalWorkflowInvalid)
		}
		r.TestCommands = append([]string{}, cfg.TestCommands...)
		r.Env = cfg.Env
		r.ReviewerConfig = cfg.Reviewers[0].AgentConfig
		r.Task = session.Metadata.Prompt
		r.LatestUserPrompt = session.Metadata.LatestUserPrompt
	}
	if r.Automatic {
		r.VerificationPlan = nil
		r.PlanState = "pending"
	}
	next := domain.WorkflowWaiting
	if r.BlockCode == "round_limit" {
		next = domain.WorkflowFixing
	}
	// A recorded user return is durable work. Losing the controller or daemon
	// before reservation must not turn Resume into verification of unchanged files.
	decisions, e := c.deps.Store.ListLocalWorkflowDecisions(ctx, r.ID)
	if e != nil {
		return Snapshot{}, e
	}
	if len(decisions) > 0 {
		decision := decisions[len(decisions)-1]
		fix := latestOperation(ops, "fix", r.Cycle)
		if decision.Kind == "changes_requested" && decision.Cycle == r.Cycle && (fix == nil || fix.InputOperationID != decision.ID) {
			if r.MaxRounds <= r.Cycle {
				return Snapshot{}, fmt.Errorf("%w: explicitly increase the round limit to continue", domain.ErrLocalWorkflowConflict)
			}
			next = domain.WorkflowFixing
		}
	}
	r.ProviderRetry = nil // An explicit human Resume starts a new bounded recovery budget.
	r.Phase = next
	r.StopRequested = false
	r.BlockCode = ""
	r.BlockMessage = ""
	r.WorkerGeneration = obs.Generation
	r.UpdatedAt = c.now()
	c.closeGuard(r.ID)
	ok, e := c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, nil)
	if e != nil {
		return Snapshot{}, e
	}
	if !ok {
		return Snapshot{}, domain.ErrLocalWorkflowConflict
	}
	return c.Get(ctx, id)
}

// Close cancels local work and closes observations within the caller deadline.
func (c *Coordinator) Close(ctx context.Context) error {
	c.cancel()
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}
	c.mu.Lock()
	for id, g := range c.guards {
		g.Close()
		delete(c.guards, id)
	}
	c.mu.Unlock()
	return nil
}

// Run sweeps durable runs until daemon shutdown.
func (c *Coordinator) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_ = c.Close(shutdown)
			cancel()
			return
		case <-tick.C:
			if err := c.ProcessAutomaticIntents(ctx); err != nil {
				c.deps.Logger.Warn("process automatic workflows", "error", err)
			}
			runs, e := c.deps.Store.ListOpenLocalWorkflows(ctx)
			if e != nil {
				c.deps.Logger.Error("list local workflows", "error", e)
				continue
			}
			for _, r := range runs {
				if e := c.Evaluate(ctx, r.ID); e != nil {
					c.deps.Logger.Warn("evaluate local workflow", "id", r.ID, "error", e)
				}
			}
		}
	}
}

// testExecutionUncertain requires durable absence or completion before retry.
func (c *Coordinator) testExecutionUncertain(ctx context.Context, id string) bool {
	state, err := c.deps.Tests.Reconcile(context.WithoutCancel(ctx), id)
	return err != nil || (state.State != "settled" && state.State != "absent")
}
