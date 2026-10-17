package localworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localsnapshot"
	"github.com/aoagents/agent-orchestrator/backend/internal/process"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type scenarioWorker struct {
	mu         sync.Mutex
	root       string
	fixes      int
	prompts    []string
	generation string
	stopped    bool
}

func (w *scenarioWorker) Observe(_ context.Context, _ domain.SessionID, id string) (WorkerObservation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	state := "idle"
	if id != "" {
		state = "completed"
	}
	if w.stopped {
		state = "interrupted"
	}
	return WorkerObservation{State: state, Generation: w.generation, TurnID: id}, nil
}
func (w *scenarioWorker) DeliverFix(_ context.Context, _ domain.SessionID, id, text string) (WorkerObservation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fixes++
	w.prompts = append(w.prompts, text)
	if e := os.WriteFile(filepath.Join(w.root, "value"), []byte("correct"), 0600); e != nil {
		return WorkerObservation{}, e
	}
	return WorkerObservation{State: "completed", Generation: w.generation, TurnID: id}, nil
}
func (w *scenarioWorker) Interrupt(context.Context, domain.SessionID, string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	return nil
}

type scenarioReviews struct {
	mu           sync.Mutex
	root         string
	alwaysReject bool
	failure      bool
	runs         map[string]domain.ReviewRun
}

func (r *scenarioReviews) TriggerWorkflowLocal(_ context.Context, run domain.LocalWorkflowRun, id, snapshot string) (domain.ReviewRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure {
		return domain.ReviewRun{}, fmt.Errorf("Claude unavailable")
	}
	if existing, ok := r.runs[id]; ok {
		return existing, nil
	}
	b, e := os.ReadFile(filepath.Join(r.root, "value"))
	if e != nil {
		return domain.ReviewRun{}, e
	}
	outcome := "approved"
	findings := []domain.LocalReviewFinding{{Kind: "suggestion", Path: "value", Line: 1, Reason: "Optional note"}}
	if string(b) != "correct" || r.alwaysReject {
		outcome = "changes_requested"
		findings = []domain.LocalReviewFinding{{Kind: "required", Path: "value", Line: 1, Reason: "Must contain correct"}}
	}
	result := domain.ReviewRun{ID: id, SessionID: run.SessionID, TargetKind: domain.ReviewTargetLocal, SnapshotID: snapshot, Round: run.Cycle, Status: domain.ReviewRunComplete, Verdict: domain.ReviewVerdict(outcome), Current: true, LocalResult: &domain.LocalReviewResult{Version: 1, Round: run.Cycle, SnapshotID: snapshot, Outcome: outcome, Summary: "Checked task", Findings: findings}}
	r.runs[id] = result
	return result, nil
}
func (r *scenarioReviews) GetWorkflowLocal(_ context.Context, id string) (domain.ReviewRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[id], nil
}
func (r *scenarioReviews) CancelWorkflowLocal(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[id]
	run.Status = domain.ReviewRunCancelled
	r.runs[id] = run
	return nil
}

type workflowFixture struct {
	store       *sqlite.Store
	worker      *scenarioWorker
	reviews     *scenarioReviews
	coordinator *Coordinator
	session     domain.SessionRecord
	data        string
	root        string
}

func setupWorkflow(t *testing.T, commands []string, maxRounds int) *workflowFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX command fixture; Windows runner is build checked")
	}
	ctx := context.Background()
	root := t.TempDir()
	cmd := process.Command("git", "init", "--quiet", root)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git init: %s %v", out, e)
	}
	if e := os.WriteFile(filepath.Join(root, "value"), []byte("wrong"), 0600); e != nil {
		t.Fatal(e)
	}
	data := t.TempDir()
	store, e := sqlitetest.Open(data)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal, TestCommands: commands, LocalWorkflowMaxRounds: maxRounds, Worker: domain.RoleOverride{Harness: domain.HarnessCodex}, Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerClaudeCode}}}
	if e = store.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: root, Config: cfg, RegisteredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	session, e := store.CreateSession(ctx, domain.SessionRecord{ProjectID: "project", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: time.Now()}, Metadata: domain.SessionMetadata{WorkspacePath: root, Prompt: "Set value to correct. Tests must pass.", ControllerGeneration: "gen"}, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	worker := &scenarioWorker{root: root, generation: "chat:gen"}
	reviews := &scenarioReviews{root: root, runs: map[string]domain.ReviewRun{}}
	c := New(Deps{Store: store, Worker: worker, Tests: NewTestRunner(data), Reviews: reviews, DataDir: data})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return &workflowFixture{store, worker, reviews, c, session, data, root}
}
func awaitPhase(t *testing.T, f *workflowFixture, phase domain.LocalWorkflowPhase) Snapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, e := f.coordinator.Get(context.Background(), f.session.ID)
		if e != nil {
			t.Fatal(e)
		}
		if snapshot.Run.Phase == phase {
			return snapshot
		}
		if snapshot.Run.Phase == domain.WorkflowBlocked && phase != domain.WorkflowBlocked {
			t.Fatalf("unexpected block: %+v", snapshot.Run)
		}
		if e = f.coordinator.Evaluate(context.Background(), snapshot.Run.ID); e != nil {
			t.Fatal(e)
		}
		time.Sleep(15 * time.Millisecond)
	}
	got, _ := f.coordinator.Get(context.Background(), f.session.ID)
	t.Fatalf("timeout waiting %s: %+v", phase, got.Run)
	return Snapshot{}
}
func TestWorkflowFullCorrectionLoop(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 0)
	ctx := context.Background()
	started, e := f.coordinator.Start(ctx, f.session.ID, "start")
	if e != nil {
		t.Fatal(e)
	}
	same, e := f.coordinator.Start(ctx, f.session.ID, "start")
	if e != nil || same.Run.ID != started.Run.ID {
		t.Fatalf("start duplicate: %+v %v", same, e)
	}
	ready := awaitPhase(t, f, domain.WorkflowReady)
	if !ready.Current || ready.Run.Cycle != 2 || ready.Run.MaxRounds != 3 || len(ready.Tests) != 2 || f.worker.fixes != 1 {
		t.Fatalf("ready=%+v fixes=%d", ready, f.worker.fixes)
	}
	assertWorkflowPresentation(t, ready, "awaiting_acceptance", 2)
	if len(ready.RequiredFindings) != 0 {
		t.Fatal("suggestion became requirement")
	}
	m, e := localsnapshot.Capture(ctx, f.root)
	if e != nil {
		t.Fatal(e)
	}
	for _, op := range ready.Operations {
		if op.Cycle == 2 && (op.Kind == "tests" || op.Kind == "review") && op.SnapshotID != m.ID {
			t.Fatal("test and review assessed different contents")
		}
	}
	if e = os.WriteFile(filepath.Join(f.root, "value"), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	stale, e := f.coordinator.Get(ctx, f.session.ID)
	assertWorkflowPresentation(t, stale, "local_review", 2)
	if e != nil || stale.Current {
		t.Fatalf("stale readiness: %+v %v", stale, e)
	}
}
func TestWorkflowLimitTestFailureAndMissingConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		commands []string
		reject   bool
		want     string
	}{{"limit", []string{"test -f value"}, true, "round_limit"}, {"test failure", []string{"exit 9"}, false, "tests_failed"}, {"missing tests", nil, false, "configuration"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupWorkflow(t, tc.commands, 1)
			f.reviews.alwaysReject = tc.reject
			_, e := f.coordinator.Start(context.Background(), f.session.ID, "start")
			if e != nil {
				t.Fatal(e)
			}
			blocked := awaitPhase(t, f, domain.WorkflowBlocked)
			if blocked.Run.BlockCode != tc.want || blocked.Current || f.worker.fixes != 0 {
				t.Fatalf("block=%+v fixes=%d", blocked.Run, f.worker.fixes)
			}
			if tc.want == "tests_failed" && len(f.reviews.runs) != 0 {
				t.Fatal("review ran after failed tests")
			}
		})
	}
}

func TestWorkflowResumeRunsNewTestsAndReviewForSameBytes(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	ctx := context.Background()
	if _, e := f.coordinator.Start(ctx, f.session.ID, "start"); e != nil {
		t.Fatal(e)
	}
	ready := awaitPhase(t, f, domain.WorkflowReady)
	stopped, e := f.coordinator.Stop(ctx, f.session.ID, ready.Run.Version)
	if e != nil {
		t.Fatal(e)
	}
	if stopped.Run.Phase != domain.WorkflowStopped {
		stopped = awaitPhase(t, f, domain.WorkflowStopped)
	}
	if _, e = f.coordinator.Resume(ctx, f.session.ID, stopped.Run.Version, nil); e != nil {
		t.Fatal(e)
	}
	second := awaitPhase(t, f, domain.WorkflowReady)
	if len(second.Tests) != 3 || len(second.Operations) != 7 {
		t.Fatalf("resumed verification reused an old operation: tests=%d operations=%+v", len(second.Tests), second.Operations)
	}
}

func TestWorkflowNewStartPreservesClosedHistory(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	ctx := context.Background()
	if _, e := f.coordinator.Start(ctx, f.session.ID, "first"); e != nil {
		t.Fatal(e)
	}
	ready := awaitPhase(t, f, domain.WorkflowReady)
	next, e := f.coordinator.Start(ctx, f.session.ID, "next")
	if e != nil {
		t.Fatal(e)
	}
	if next.Run.ID == ready.Run.ID {
		t.Fatal("new run overwrote previous workflow")
	}
	old, ok, e := f.store.GetLocalWorkflow(ctx, ready.Run.ID)
	if e != nil || !ok || old.ClosedAt == nil {
		t.Fatalf("history not closed: %+v %v", old, e)
	}
}

func TestWorkflowChangedWorkerDoesNotReceiveFix(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	ctx := context.Background()
	if _, e := f.coordinator.Start(ctx, f.session.ID, "start"); e != nil {
		t.Fatal(e)
	}
	fixing := awaitPhase(t, f, domain.WorkflowFixing)
	f.worker.mu.Lock()
	f.worker.generation = "chat:replacement"
	f.worker.mu.Unlock()
	if e := f.coordinator.Evaluate(ctx, fixing.Run.ID); e != nil {
		t.Fatal(e)
	}
	blocked := awaitPhase(t, f, domain.WorkflowBlocked)
	f.worker.mu.Lock()
	fixes := f.worker.fixes
	f.worker.mu.Unlock()
	if blocked.Run.BlockCode != "worker_changed" || fixes != 0 {
		t.Fatalf("replacement received an old correction: %+v fixes=%d", blocked.Run, fixes)
	}
}

type emptyTestRunner struct{ TestRunner }

func (emptyTestRunner) Run(context.Context, TestPlan) ([]domain.LocalWorkflowTestResult, error) {
	return nil, nil
}
func TestWorkflowRejectsIncompleteTestEvidence(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	f.coordinator.deps.Tests = emptyTestRunner{NewTestRunner(f.data)}
	ctx := context.Background()
	start, e := f.coordinator.Start(ctx, f.session.ID, "start")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.coordinator.Evaluate(ctx, start.Run.ID); e != nil {
		t.Fatal(e)
	}
	// A successful return without one result per configured command is not proof.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap, e := f.coordinator.Get(ctx, f.session.ID)
		if e != nil {
			t.Fatal(e)
		}
		// Inspect one durable snapshot: the runner can finish after Get but
		// before an isActive check, leaving the earlier phase stale.
		if snap.Run.Phase != domain.WorkflowTesting {
			if snap.Run.Phase != domain.WorkflowBlocked || snap.Run.BlockCode != "tests_failed" {
				t.Fatalf("incomplete evidence advanced: %+v", snap.Run)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("runner did not settle")
}

type unavailableWorker struct{ Worker }

func (unavailableWorker) Observe(context.Context, domain.SessionID, string) (WorkerObservation, error) {
	return WorkerObservation{}, fmt.Errorf("executor state unavailable")
}
func TestWorkflowUnavailableExecutorHasDurableBlock(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	f.coordinator.deps.Worker = unavailableWorker{f.worker}
	snapshot, e := f.coordinator.Start(context.Background(), f.session.ID, "start")
	if e != nil {
		t.Fatalf("executor error should be a durable block: %v", e)
	}
	if snapshot.Run == nil || snapshot.Run.Phase != domain.WorkflowBlocked || snapshot.Run.BlockCode != "executor_unavailable" {
		t.Fatalf("missing block: %+v", snapshot)
	}
	persisted, _, e := f.store.CurrentLocalWorkflow(context.Background(), f.session.ID)
	if e != nil || persisted.BlockCode != "executor_unavailable" {
		t.Fatalf("not durable: %+v %v", persisted, e)
	}
}

// The cancellation boundary must be able to enter the same workspace admission
// used by Chat Send; Stop must not hold it while waiting for the controller.
type admissionStopWorker struct {
	*scenarioWorker
	store *sqlite.Store
}

func (w *admissionStopWorker) Observe(context.Context, domain.SessionID, string) (WorkerObservation, error) {
	return WorkerObservation{State: "running", Generation: "chat:gen"}, nil
}
func (w *admissionStopWorker) Interrupt(ctx context.Context, id domain.SessionID, _ string) error {
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	release, err := w.store.AcquireLocalReviewWorkspace(ctx, id)
	if err != nil {
		return err
	}
	release()
	return nil
}
func TestStopReleasesWorkspaceBeforeControllerCancellation(t *testing.T) {
	f := setupWorkflow(t, []string{"exit 0"}, 3)
	f.coordinator.deps.Worker = &admissionStopWorker{f.worker, f.store}
	started, err := f.coordinator.Start(context.Background(), f.session.ID, "stop-lock")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.Stop(context.Background(), f.session.ID, started.Run.Version); err != nil {
		t.Fatalf("controller cancellation could not acquire admission: %v", err)
	}
}

type uncertainStopRunner struct{ started chan struct{} }

func (r *uncertainStopRunner) Run(ctx context.Context, _ TestPlan) ([]domain.LocalWorkflowTestResult, error) {
	close(r.started)
	<-ctx.Done()
	return nil, fmt.Errorf("host cancellation uncertain")
}
func (*uncertainStopRunner) Cancel(context.Context, string) error { return nil }
func (*uncertainStopRunner) Reconcile(context.Context, string) (TestProcessState, error) {
	return TestProcessState{State: "uncertain"}, nil
}
func TestStopPreservesUncertainTestExecution(t *testing.T) {
	f := setupWorkflow(t, []string{"exit 0"}, 3)
	runner := &uncertainStopRunner{started: make(chan struct{})}
	f.coordinator.deps.Tests = runner
	started, err := f.coordinator.Start(context.Background(), f.session.ID, "uncertain-stop")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.coordinator.Evaluate(context.Background(), started.Run.ID); err != nil {
		t.Fatal(err)
	}
	<-runner.started
	running, err := f.coordinator.Get(context.Background(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.Stop(context.Background(), f.session.ID, running.Run.Version); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snap, err := f.coordinator.Get(context.Background(), f.session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Operations) > 0 && !f.coordinator.isActive(snap.Operations[0].ID) {
			// The callback can settle after Get and before isActive. Read its
			// durable result only after observing callback completion.
			snap, err = f.coordinator.Get(context.Background(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if snap.Operations[0].State != "uncertain" {
				t.Fatalf("lost execution uncertainty: %+v", snap.Operations[0])
			}
			if err = f.coordinator.Evaluate(context.Background(), snap.Run.ID); err != nil {
				t.Fatal(err)
			}
			snap, err = f.coordinator.Get(context.Background(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if snap.Run.Phase == domain.WorkflowStopped {
				t.Fatal("claimed stopped without settled host")
			}
			if _, err = f.coordinator.Resume(context.Background(), f.session.ID, snap.Run.Version, nil); err == nil {
				t.Fatal("resumed uncertain test")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("test callback did not settle")
}

func TestDeadTestHostDoesNotAllowOverlappingRetry(t *testing.T) {
	f := setupWorkflow(t, []string{"echo $$ > child.pid; exec sleep 30"}, 3)
	started, err := f.coordinator.Start(context.Background(), f.session.ID, "host-death")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.coordinator.Evaluate(context.Background(), started.Run.ID); err != nil {
		t.Fatal(err)
	}
	var op domain.LocalWorkflowOperation
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap, err := f.coordinator.Get(context.Background(), f.session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Operations) > 0 {
			op = snap.Operations[0]
		}
		if _, err = os.Stat(filepath.Join(f.root, "child.pid")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	childData, err := os.ReadFile(filepath.Join(f.root, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	var childPID int
	if _, err = fmt.Sscanf(string(childData), "%d", &childPID); err != nil {
		t.Fatal(err)
	}
	child, err := os.FindProcess(childPID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Kill() }()
	ownerData, err := os.ReadFile(filepath.Join(f.data, "local-workflow", "tests", op.ID, "owner.json"))
	if err != nil {
		t.Fatal(err)
	}
	var owner struct{ PID int }
	if err = json.Unmarshal(ownerData, &owner); err != nil {
		t.Fatal(err)
	}
	host, err := os.FindProcess(owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Kill(); err != nil {
		t.Fatal(err)
	}
	blocked := awaitPhase(t, f, domain.WorkflowBlocked)
	if blocked.Operations[0].State != "uncertain" {
		t.Fatalf("dead host marked terminal while command survives: %+v", blocked.Operations[0])
	}
	if _, err = f.coordinator.Resume(context.Background(), f.session.ID, blocked.Run.Version, nil); err == nil {
		t.Fatal("launched overlapping retry")
	}
	if _, err = f.coordinator.Stop(context.Background(), f.session.ID, blocked.Run.Version); err != nil {
		t.Fatal(err)
	}
	after, err := f.coordinator.Get(context.Background(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Run.Phase == domain.WorkflowStopped {
		t.Fatal("Stop forgot surviving command")
	}
}

// Reading the wire model catches missing history and premature board readiness.
func assertWorkflowPresentation(t *testing.T, snapshot Snapshot, stage string, reviews int) {
	t.Helper()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Stage   string `json:"stage"`
		Reviews []struct {
			ID          string                    `json:"id"`
			LocalResult *domain.LocalReviewResult `json:"localResult"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Stage != stage || len(wire.Reviews) != reviews {
		t.Fatalf("stage=%q reviews=%d; want %s and %d", wire.Stage, len(wire.Reviews), stage, reviews)
	}
	if reviews == 2 && (wire.Reviews[0].LocalResult == nil || wire.Reviews[0].LocalResult.Outcome != "changes_requested" || wire.Reviews[1].LocalResult == nil || wire.Reviews[1].LocalResult.Outcome != "approved") {
		t.Fatalf("lost round results: %s", encoded)
	}
}
