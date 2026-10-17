// Package localworkflow coordinates durable, explicitly started local work.
package localworkflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localsnapshot"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Store persists workflow facts, atomic checkpoints and operation reservations.
type Store interface {
	ports.LocalReviewWorkspace
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	CreateLocalWorkflow(context.Context, domain.LocalWorkflowRun, string) (domain.LocalWorkflowRun, bool, error)
	GetLocalWorkflow(context.Context, string) (domain.LocalWorkflowRun, bool, error)
	CurrentLocalWorkflow(context.Context, domain.SessionID) (domain.LocalWorkflowRun, bool, error)
	ListOpenLocalWorkflows(context.Context) ([]domain.LocalWorkflowRun, error)
	AdvanceLocalWorkflow(context.Context, int64, domain.LocalWorkflowRun, *domain.LocalWorkflowOperation) (bool, error)
	CompleteLocalWorkflowOperation(context.Context, int64, domain.LocalWorkflowOperation, domain.LocalWorkflowRun, []domain.LocalWorkflowTestResult) (bool, error)
	ListLocalWorkflowOperations(context.Context, string) ([]domain.LocalWorkflowOperation, error)
	ListLocalWorkflowTests(context.Context, string) ([]domain.LocalWorkflowTestResult, error)
	ListLocalWorkflowDecisions(context.Context, string) ([]domain.LocalWorkflowDecision, error)
	LocalWorkflowDecisionByRequest(context.Context, string, string) (domain.LocalWorkflowDecision, bool, error)
	RecordLocalWorkflowDecision(context.Context, int64, domain.LocalWorkflowRun, domain.LocalWorkflowDecision) (bool, error)
}

// Reviews is the independent local reviewer boundary consumed by the coordinator.
type Reviews interface {
	TriggerWorkflowLocal(context.Context, domain.LocalWorkflowRun, string, string) (domain.ReviewRun, error)
	GetWorkflowLocal(context.Context, string) (domain.ReviewRun, error)
	CancelWorkflowLocal(context.Context, string) error
}

// Snapshot is a read model of the current workflow and its accumulated evidence.
type Snapshot struct {
	Decisions        []domain.LocalWorkflowDecision   `json:"decisions"`
	Accepted         bool                             `json:"accepted"`
	Stage            string                           `json:"stage" enum:"executing,testing,local_review,fixing,awaiting_acceptance,accepted"`
	Reviews          []domain.ReviewRun               `json:"reviews"`
	Run              *domain.LocalWorkflowRun         `json:"run"`
	Operations       []domain.LocalWorkflowOperation  `json:"operations"`
	Tests            []domain.LocalWorkflowTestResult `json:"tests"`
	Current          bool                             `json:"current"`
	RequiredFindings []domain.LocalReviewFinding      `json:"requiredFindings"`
}

// Deps supplies durable storage, worker, test and review boundaries.
type Deps struct {
	Decisions DecisionConfirmer
	Store     Store
	Worker    Worker
	Tests     TestRunner
	Reviews   Reviews
	DataDir   string
	Clock     func() time.Time
	Logger    *slog.Logger
}

// Coordinator owns local workflow transitions and live content observations.
type Coordinator struct {
	deps   Deps
	ctx    context.Context
	cancel context.CancelFunc
	locks  sync.Map
	mu     sync.Mutex
	guards map[string]*localsnapshot.Guard
	active map[string]context.CancelFunc
	wg     sync.WaitGroup
}

// New constructs a coordinator without starting work or recovery.
func New(deps Deps) *Coordinator {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Coordinator{deps: deps, ctx: ctx, cancel: cancel, guards: map[string]*localsnapshot.Guard{}, active: map[string]context.CancelFunc{}}
}
func (c *Coordinator) now() time.Time { return c.deps.Clock().UTC() }
func (c *Coordinator) lock(id string) func() {
	v, _ := c.locks.LoadOrStore(id, &sync.Mutex{})
	m, ok := v.(*sync.Mutex)
	if !ok {
		panic("invalid local workflow mutex")
	}
	m.Lock()
	return m.Unlock
}
func (c *Coordinator) guard(id string) *localsnapshot.Guard {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.guards[id]
}
func (c *Coordinator) closeGuard(id string) {
	c.mu.Lock()
	g := c.guards[id]
	delete(c.guards, id)
	c.mu.Unlock()
	if g != nil {
		g.Close()
	}
}
func (c *Coordinator) checkGuard(ctx context.Context, id string) error {
	g := c.guard(id)
	if g == nil {
		return fmt.Errorf("workspace observation continuity was lost; explicitly resume to run fresh tests")
	}
	return g.Check(ctx)
}
func (c *Coordinator) isActive(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.active[id]
	return ok
}
func (c *Coordinator) launch(id string, fn func(context.Context)) {
	ctx, cancel := context.WithCancel(c.ctx)
	c.mu.Lock()
	c.active[id] = cancel
	c.wg.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.wg.Done()
		defer cancel()
		defer func() { c.mu.Lock(); delete(c.active, id); c.mu.Unlock() }()
		fn(ctx)
	}()
}
func (c *Coordinator) block(ctx context.Context, r domain.LocalWorkflowRun, code string, cause error) error {
	r.Phase = domain.WorkflowBlocked
	r.BlockCode = code
	r.BlockMessage = cause.Error()
	r.UpdatedAt = c.now()
	_, e := c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, nil)
	c.closeGuard(r.ID)
	return e
}

// Start creates an explicitly requested run, preserving previous completed history.
func (c *Coordinator) Start(ctx context.Context, id domain.SessionID, requestID string) (Snapshot, error) {
	if !safeOperationID(requestID) {
		return Snapshot{}, fmt.Errorf("%w: requestId must be a non-empty opaque identifier", domain.ErrLocalWorkflowInvalid)
	}
	unlock := c.lock("session:" + string(id))
	defer unlock()
	session, ok, e := c.deps.Store.GetSession(ctx, id)
	if e != nil {
		return Snapshot{}, e
	}
	if !ok {
		return Snapshot{}, domain.ErrLocalWorkflowNotFound
	}
	existing, found, e := c.deps.Store.CurrentLocalWorkflow(ctx, id)
	if e != nil {
		return Snapshot{}, e
	}
	if found && existing.RequestID == requestID {
		return c.snapshot(ctx, existing)
	}
	if found && existing.ClosedAt == nil {
		if existing.Phase != domain.WorkflowReady && existing.Phase != domain.WorkflowStopped {
			return Snapshot{}, fmt.Errorf("%w: session already has a workflow; use Stop or Resume", domain.ErrLocalWorkflowConflict)
		}
		release := c.lock(existing.ID)
		now := c.now()
		existing.ClosedAt = &now
		existing.UpdatedAt = now
		changed, err := c.deps.Store.AdvanceLocalWorkflow(ctx, existing.Version, existing, nil)
		release()
		if err != nil {
			return Snapshot{}, err
		}
		if !changed {
			return Snapshot{}, domain.ErrLocalWorkflowConflict
		}
		c.closeGuard(existing.ID)
	}
	project, ok, e := c.deps.Store.GetProject(ctx, string(session.ProjectID))
	if e != nil {
		return Snapshot{}, e
	}
	if !ok {
		return Snapshot{}, fmt.Errorf("%w: project is missing", domain.ErrLocalWorkflowInvalid)
	}
	cfg := project.Config.WithDefaults()
	observation, observationErr := c.deps.Worker.Observe(ctx, id, "")
	if observation.Generation == "" {
		observation.Generation = domain.LocalWorkflowWorkerGeneration(session)
	}
	r := domain.LocalWorkflowRun{ID: uuid.NewString(), SessionID: id, Phase: domain.WorkflowWaiting, Cycle: 1, MaxRounds: cfg.EffectiveLocalWorkflowMaxRounds(), Task: session.Metadata.Prompt, LatestUserPrompt: session.Metadata.LatestUserPrompt, TestCommands: append([]string{}, cfg.TestCommands...), Env: cfg.Env, WorkspacePath: session.Metadata.WorkspacePath, WorkerGeneration: observation.Generation, CreatedAt: c.now(), UpdatedAt: c.now()}
	if len(cfg.Reviewers) == 1 {
		r.ReviewerConfig = cfg.Reviewers[0].AgentConfig
	}
	invalid := ""
	switch {
	case cfg.EffectiveReviewMode() != domain.ReviewTargetLocal:
		invalid = "Project must use Local review"
	case session.Harness != domain.HarnessCodex:
		invalid = "Local workflow requires a Codex executor"
	case len(cfg.Reviewers) != 1 || cfg.Reviewers[0].Harness != domain.ReviewerClaudeCode:
		invalid = "Local workflow requires one Claude Code reviewer"
	case len(cfg.TestCommands) == 0:
		invalid = "Configure at least one test command before starting the workflow"
	case session.IsTerminated:
		invalid = "Executor is terminated"
	case r.Task == "" && r.LatestUserPrompt == "":
		invalid = "Executor task and acceptance criteria are missing"
	}
	if err := cfg.Validate(); err != nil {
		invalid = err.Error()
	}
	if invalid != "" {
		r.Phase = domain.WorkflowBlocked
		r.BlockCode = "configuration"
		r.BlockMessage = invalid
	}
	if invalid == "" && observationErr != nil {
		r.Phase = domain.WorkflowBlocked
		r.BlockCode = "executor_unavailable"
		r.BlockMessage = observationErr.Error()
	}

	r, _, e = c.deps.Store.CreateLocalWorkflow(ctx, r, requestID)
	if e != nil {
		return Snapshot{}, e
	}
	return c.snapshot(ctx, r)
}

// Get reads the latest run and derives current readiness from live evidence.
func (c *Coordinator) Get(ctx context.Context, id domain.SessionID) (Snapshot, error) {
	if _, ok, e := c.deps.Store.GetSession(ctx, id); e != nil {
		return Snapshot{}, e
	} else if !ok {
		return Snapshot{}, domain.ErrLocalWorkflowNotFound
	}
	r, ok, e := c.deps.Store.CurrentLocalWorkflow(ctx, id)
	if e != nil {
		return Snapshot{}, e
	}
	if !ok {
		return Snapshot{Decisions: []domain.LocalWorkflowDecision{}, Stage: "executing", Reviews: []domain.ReviewRun{}, Operations: []domain.LocalWorkflowOperation{}, Tests: []domain.LocalWorkflowTestResult{}, RequiredFindings: []domain.LocalReviewFinding{}}, nil
	}
	return c.snapshot(ctx, r)
}
func (c *Coordinator) snapshot(ctx context.Context, r domain.LocalWorkflowRun) (Snapshot, error) {
	ops, e := c.deps.Store.ListLocalWorkflowOperations(ctx, r.ID)
	if e != nil {
		return Snapshot{}, e
	}
	tests, e := c.deps.Store.ListLocalWorkflowTests(ctx, r.ID)
	if e != nil {
		return Snapshot{}, e
	}
	snap := Snapshot{Run: &r, Operations: ops, Tests: tests, Reviews: []domain.ReviewRun{}, RequiredFindings: []domain.LocalReviewFinding{}}
	latestReview := latestOperation(ops, "review", 0)
	for _, op := range ops {
		if op.Kind != "review" || op.ReviewRunID == "" {
			continue
		}
		review, err := c.deps.Reviews.GetWorkflowLocal(ctx, op.ReviewRunID)
		if err != nil {
			return snap, err
		}
		if review.ID != "" {
			snap.Reviews = append(snap.Reviews, review)
		}
		if latestReview == nil || op.ID != latestReview.ID {
			continue
		}
		if review.LocalResult != nil {
			for _, finding := range review.LocalResult.Findings {
				if finding.Kind == "required" {
					snap.RequiredFindings = append(snap.RequiredFindings, finding)
				}
			}
		}
	}
	snap.Decisions, e = c.deps.Store.ListLocalWorkflowDecisions(ctx, r.ID)
	if e != nil {
		return snap, e
	}
	snap.Current = currentEvidence(snap) && c.checkGuard(ctx, r.ID) == nil
	snap.Accepted = acceptedEvidence(snap)
	snap.Stage = workflowStage(snap)

	return snap, nil
}
func latestOperation(ops []domain.LocalWorkflowOperation, kind string, cycle int) *domain.LocalWorkflowOperation {
	for i := len(ops) - 1; i >= 0; i-- {
		if ops[i].Kind == kind && (cycle == 0 || ops[i].Cycle == cycle) {
			operation := ops[i]
			return &operation
		}
	}
	return nil
}
func (c *Coordinator) newOperation(r domain.LocalWorkflowRun, ops []domain.LocalWorkflowOperation, kind string) domain.LocalWorkflowOperation {
	attempt := 1
	for _, op := range ops {
		if op.Cycle == r.Cycle && op.Kind == kind && op.Attempt >= attempt {
			attempt = op.Attempt + 1
		}
	}
	id := uuid.NewString()
	deliveryID := ""
	if kind == "fix" {
		deliveryID = id
	}
	return domain.LocalWorkflowOperation{ID: id, WorkflowID: r.ID, Kind: kind, Cycle: r.Cycle, Attempt: attempt, State: "reserved", Generation: r.WorkerGeneration, ClientMessageID: deliveryID, StartedAt: c.now()}
}

// Evaluate advances one checkpoint without replaying reserved side effects.
func (c *Coordinator) Evaluate(ctx context.Context, id string) error {
	unlock := c.lock(id)
	defer unlock()
	r, ok, e := c.deps.Store.GetLocalWorkflow(ctx, id)
	if e != nil || !ok {
		return e
	}
	if r.ClosedAt != nil {
		return nil
	}
	ops, e := c.deps.Store.ListLocalWorkflowOperations(ctx, id)
	if e != nil {
		return e
	}
	switch r.Phase {
	case domain.WorkflowBlocked, domain.WorkflowStopped:
		return nil
	case domain.WorkflowStopping:
		return c.finishStop(ctx, r, ops)
	case domain.WorkflowReady:
		if e = c.checkGuard(ctx, id); e != nil {
			return c.block(ctx, r, "workspace_changed", e)
		}
		return nil
	case domain.WorkflowWaiting:
		obs, e := c.deps.Worker.Observe(ctx, r.SessionID, "")
		if e != nil {
			return c.block(ctx, r, "executor_error", e)
		}
		if obs.Generation != r.WorkerGeneration {
			return c.block(ctx, r, "worker_changed", fmt.Errorf("executor controller changed; explicitly resume"))
		}
		if obs.State == "running" || obs.State == "queued" {
			return nil
		}
		if !workerVerificationAllowed(r, obs) {
			return c.block(ctx, r, "executor_"+obs.State, fmt.Errorf("executor is %s: %s", obs.State, obs.Error))
		}
		return c.beginTests(ctx, r, ops)
	case domain.WorkflowTesting:
		if op := latestOperation(ops, "tests", r.Cycle); op != nil && !c.isActive(op.ID) {
			return c.block(ctx, r, "tests_uncertain", fmt.Errorf("test runner ended without a confirmed state transition; explicitly resume"))
		}
		return nil
	case domain.WorkflowReviewing:
		if e = c.checkGuard(ctx, id); e != nil {
			return c.block(ctx, r, "workspace_changed", e)
		}
		op := latestOperation(ops, "review", r.Cycle)
		test := latestOperation(ops, "tests", r.Cycle)
		if op == nil || test != nil && op.InputOperationID != test.ID {
			return c.beginReview(ctx, r, ops)
		}
		if c.isActive(op.ID) {
			return nil
		}
		if op.ReviewRunID == "" {
			return c.block(ctx, r, "review_uncertain", fmt.Errorf("review launch outcome is uncertain; explicitly resume"))
		}
		result, e := c.deps.Reviews.GetWorkflowLocal(ctx, op.ReviewRunID)
		if e != nil {
			return c.block(ctx, r, "review_error", e)
		}
		return c.consumeReview(ctx, r, *op, result)
	case domain.WorkflowFixing:
		op := latestOperation(ops, "fix", r.Cycle)
		input, _, err := c.fixInput(ctx, r, ops)
		if err != nil {
			return c.block(ctx, r, "review_error", err)
		}
		if op == nil || op.InputOperationID != input {
			return c.beginFix(ctx, r, ops)
		}
		if c.isActive(op.ID) {
			return nil
		}
		obs, e := c.deps.Worker.Observe(ctx, r.SessionID, op.ID)
		if e != nil {
			return c.block(ctx, r, "executor_error", e)
		}
		return c.consumeFix(ctx, r, *op, obs)
	}
	return nil
}
func (c *Coordinator) reserve(ctx context.Context, r domain.LocalWorkflowRun, phase domain.LocalWorkflowPhase, op domain.LocalWorkflowOperation) (domain.LocalWorkflowRun, bool, error) {
	r.Phase = phase
	r.UpdatedAt = c.now()
	ok, e := c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, &op)
	if ok {
		r.Version++
	}
	return r, ok, e
}
func (c *Coordinator) beginTests(ctx context.Context, r domain.LocalWorkflowRun, ops []domain.LocalWorkflowOperation) error {
	release, e := c.deps.Store.AcquireLocalReviewWorkspace(ctx, r.SessionID)
	if e != nil {
		return e
	}
	defer release()
	pending, e := c.deps.Store.HasPendingLocalReviewWork(ctx, r.SessionID)
	if e != nil {
		return e
	}
	obs, e := c.deps.Worker.Observe(ctx, r.SessionID, "")
	if e != nil {
		return e
	}
	if pending || !workerVerificationAllowed(r, obs) {
		return nil
	}
	manifest, e := localsnapshot.Capture(ctx, r.WorkspacePath)
	if e != nil {
		return c.block(ctx, r, "snapshot_error", e)
	}
	guard, e := localsnapshot.Watch(r.WorkspacePath, manifest)
	if e != nil {
		return c.block(ctx, r, "snapshot_error", e)
	}
	if e = guard.Check(ctx); e != nil {
		guard.Close()
		return c.block(ctx, r, "workspace_changed", e)
	}
	op := c.newOperation(r, ops, "tests")
	op.SnapshotID = manifest.ID
	op.ObservationID = manifest.ObservationID
	next, ok, e := c.reserve(ctx, r, domain.WorkflowTesting, op)
	if e != nil || !ok {
		guard.Close()
		return e
	}
	c.closeGuard(r.ID)
	c.mu.Lock()
	c.guards[r.ID] = guard
	c.mu.Unlock()
	c.launch(op.ID, func(background context.Context) {
		results, runErr := c.deps.Tests.Run(background, TestPlan{OperationID: op.ID, WorkspacePath: r.WorkspacePath, Commands: r.TestCommands, Env: r.Env, DataDir: c.deps.DataDir})
		if runErr == nil {
			runErr = validateTestEvidence(r, op, results)
		}

		unlock := c.lock(r.ID)
		defer unlock()
		latest, ok, e := c.deps.Store.GetLocalWorkflow(context.WithoutCancel(background), r.ID)
		if e != nil || !ok {
			return
		}
		if latest.Version != next.Version || latest.StopRequested {
			c.settleInterrupted(background, latest, op, results)
			return
		}
		op.State = "succeeded"
		at := c.now()
		op.CompletedAt = &at
		latest.Phase = domain.WorkflowReviewing
		if runErr == nil {
			runErr = c.checkGuard(background, r.ID)
		}
		if runErr != nil {
			op.State = "failed"
			if c.testExecutionUncertain(background, op.ID) {
				op.State = "uncertain"
				op.CompletedAt = nil
				op.ErrorCode = "execution_uncertain"
			}
			latest.Phase = domain.WorkflowBlocked
			latest.BlockCode = "tests_failed"
			latest.BlockMessage = runErr.Error()
			op.ErrorMessage = runErr.Error()
			c.closeGuard(r.ID)
		}
		latest.UpdatedAt = c.now()
		if _, e = c.deps.Store.CompleteLocalWorkflowOperation(context.WithoutCancel(background), latest.Version, op, latest, results); e != nil {
			c.deps.Logger.Error("record workflow tests", "operation", op.ID, "error", e)
		}
	})
	return nil
}
func (c *Coordinator) beginReview(ctx context.Context, r domain.LocalWorkflowRun, ops []domain.LocalWorkflowOperation) error {
	test := latestOperation(ops, "tests", r.Cycle)
	if test == nil || test.State != "succeeded" {
		return c.block(ctx, r, "tests_missing", fmt.Errorf("successful tests for this cycle are missing"))
	}
	op := c.newOperation(r, ops, "review")
	op.SnapshotID = test.SnapshotID
	op.ObservationID = test.ObservationID
	op.InputOperationID = test.ID
	_, ok, e := c.reserve(ctx, r, domain.WorkflowReviewing, op)
	if e != nil || !ok {
		return e
	}
	c.launch(op.ID, func(background context.Context) {
		result, launchErr := c.deps.Reviews.TriggerWorkflowLocal(background, r, op.ID, op.SnapshotID)
		unlock := c.lock(r.ID)
		defer unlock()
		latest, ok, e := c.deps.Store.GetLocalWorkflow(context.WithoutCancel(background), r.ID)
		if e != nil || !ok {
			return
		}
		if latest.StopRequested || latest.Phase != domain.WorkflowReviewing {
			if result.ID != "" {
				_ = c.deps.Reviews.CancelWorkflowLocal(context.WithoutCancel(background), result.ID)
			}
			op.ReviewRunID = result.ID
			c.settleInterrupted(background, latest, op, nil)
			return
		}
		op.ReviewRunID = result.ID
		op.State = "running"
		if launchErr != nil {
			op.State = "failed"
			latest.Phase = domain.WorkflowBlocked
			latest.BlockCode = "review_error"
			latest.BlockMessage = launchErr.Error()
			at := c.now()
			op.CompletedAt = &at
			c.closeGuard(r.ID)
		}
		latest.UpdatedAt = c.now()
		if _, e = c.deps.Store.CompleteLocalWorkflowOperation(context.WithoutCancel(background), latest.Version, op, latest, nil); e != nil {
			c.deps.Logger.Error("record workflow reviewer", "error", e)
		}
	})
	return nil
}
func (c *Coordinator) consumeReview(ctx context.Context, r domain.LocalWorkflowRun, op domain.LocalWorkflowOperation, result domain.ReviewRun) error {
	if result.Status == domain.ReviewRunRunning {
		return nil
	}
	var cause error
	if (result.Status != domain.ReviewRunComplete && result.Status != domain.ReviewRunDelivered) || !result.Current || result.LocalResult == nil {
		cause = fmt.Errorf("review did not produce an actual current result: %s", result.Status)
	} else if result.SnapshotID != op.SnapshotID {
		cause = fmt.Errorf("review snapshot differs from tested files")
	} else {
		cause = result.LocalResult.Validate(op.SnapshotID, result.Round)
	}
	if cause != nil {
		return c.block(ctx, r, "review_error", cause)
	}
	if result.LocalResult.Outcome == "error" {
		return c.block(ctx, r, "review_error", errors.New(result.LocalResult.Summary))
	}
	op.State = "succeeded"
	at := c.now()
	op.CompletedAt = &at
	r.UpdatedAt = at
	switch result.Verdict {
	case domain.VerdictApproved:
		r.Phase = domain.WorkflowReady
	case domain.VerdictChangesRequested:
		c.closeGuard(r.ID)
		if r.Cycle >= r.MaxRounds {
			r.Phase = domain.WorkflowBlocked
			r.BlockCode = "round_limit"
			r.BlockMessage = "Round limit reached. Required findings remain; explicitly increase the limit to continue."
		} else {
			r.Phase = domain.WorkflowFixing
		}
	default:
		return c.block(ctx, r, "review_error", fmt.Errorf("review has no actionable verdict"))
	}
	_, e := c.deps.Store.CompleteLocalWorkflowOperation(ctx, r.Version, op, r, nil)
	return e
}
func (c *Coordinator) beginFix(ctx context.Context, r domain.LocalWorkflowRun, ops []domain.LocalWorkflowOperation) error {
	obs, err := c.deps.Worker.Observe(ctx, r.SessionID, "")
	if err != nil {
		return c.block(ctx, r, "executor_error", err)
	}
	if obs.Generation != r.WorkerGeneration {
		return c.block(ctx, r, "worker_changed", fmt.Errorf("executor controller changed before correction dispatch; explicitly resume"))
	}
	if obs.State != "idle" && obs.State != "completed" {
		return c.block(ctx, r, "executor_"+obs.State, fmt.Errorf("executor is %s before corrections", obs.State))
	}

	input, text, e := c.fixInput(ctx, r, ops)
	if e != nil {
		return c.block(ctx, r, "review_error", e)
	}
	op := c.newOperation(r, ops, "fix")
	if review := latestOperation(ops, "review", r.Cycle); review != nil {
		op.SnapshotID = review.SnapshotID
	}
	op.InputOperationID = input
	_, ok, e := c.reserve(ctx, r, domain.WorkflowFixing, op)
	if e != nil || !ok {
		return e
	}
	c.launch(op.ID, func(background context.Context) {
		obs, sendErr := c.deps.Worker.DeliverFix(background, r.SessionID, op.ID, text)
		unlock := c.lock(r.ID)
		defer unlock()
		latest, ok, e := c.deps.Store.GetLocalWorkflow(context.WithoutCancel(background), r.ID)
		if e != nil || !ok {
			return
		}
		if latest.StopRequested {
			_ = c.deps.Worker.Interrupt(context.WithoutCancel(background), r.SessionID, op.ID)
			return
		}
		if sendErr != nil {
			_ = c.block(context.WithoutCancel(background), latest, "executor_error", sendErr)
			return
		}
		if e = c.consumeFix(context.WithoutCancel(background), latest, op, obs); e != nil {
			c.deps.Logger.Error("record workflow fix", "error", e)
		}
	})
	return nil
}
func (c *Coordinator) consumeFix(ctx context.Context, r domain.LocalWorkflowRun, op domain.LocalWorkflowOperation, obs WorkerObservation) error {
	if obs.Generation != op.Generation {
		return c.block(ctx, r, "worker_changed", fmt.Errorf("executor controller changed during corrections; explicitly resume"))
	}
	switch obs.State {
	case "running", "queued":
		if op.State == "running" {
			return nil
		}
		op.State = "running"
		op.WorkerTurnID = obs.TurnID
	case "completed":
		op.State = "succeeded"
		op.WorkerTurnID = obs.TurnID
		at := c.now()
		op.CompletedAt = &at
		r.Cycle++
		r.Phase = domain.WorkflowWaiting
	default:
		return c.block(ctx, r, "executor_"+obs.State, fmt.Errorf("correction is %s: %s", obs.State, obs.Error))
	}
	r.UpdatedAt = c.now()
	_, e := c.deps.Store.CompleteLocalWorkflowOperation(ctx, r.Version, op, r, nil)
	return e
}

func workerVerificationAllowed(r domain.LocalWorkflowRun, obs WorkerObservation) bool {
	if obs.State == "idle" || obs.State == "completed" {
		return true
	}
	return (obs.State == "failed" || obs.State == "interrupted") && obs.TurnID != "" && obs.TurnID == r.VerificationAfterTurnID
}

func validateTestEvidence(r domain.LocalWorkflowRun, op domain.LocalWorkflowOperation, results []domain.LocalWorkflowTestResult) error {
	if len(results) != len(r.TestCommands) {
		return fmt.Errorf("test runner returned %d results for %d commands", len(results), len(r.TestCommands))
	}
	for i, result := range results {
		if result.OperationID != op.ID || result.CommandIndex != i || result.Command != r.TestCommands[i] || result.Status != "succeeded" || result.ExitCode == nil || *result.ExitCode != 0 || result.CompletedAt == nil {
			return fmt.Errorf("test command %d has incomplete or unsuccessful evidence", i+1)
		}
	}
	return nil
}
