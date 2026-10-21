// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).
package localworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestPlannerUsesLatestInstructionAndOnlyAvailableChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"vitest run","lint":"eslint .","deploy":"publish"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := ModelPlanner{Generate: func(_ context.Context, _ domain.SessionID, system, prompt string) (string, error) {
		if !strings.Contains(prompt, "Implement the approved fixes") || !strings.Contains(prompt, "44 tests") || strings.Contains(prompt, "npm run deploy") {
			t.Fatalf("wrong planning context: %s", prompt)
		}
		return `{"profile":"implementation","rationale":"Code was changed","commands":["CI=1 npm run test","npm run lint"],"checks":["regressions","static analysis"]}`, nil
	}}
	plan, err := p.Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root, Task: "Audit and report", LatestUserPrompt: "Implement the approved fixes", ExecutionOutput: "44 tests"})
	if err != nil || plan.Profile != "implementation" || len(plan.Commands) != 2 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	p.Generate = func(context.Context, domain.SessionID, string, string) (string, error) {
		return `{"profile":"implementation","rationale":"code","commands":["rm -rf ."],"checks":["test"]}`, nil
	}
	if _, err = p.Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root}); err == nil {
		t.Fatal("invented command accepted")
	}
}

func TestReportPlanNeedsContentEvidenceAndDoesNotInventTests(t *testing.T) {
	p := ModelPlanner{Generate: func(context.Context, domain.SessionID, string, string) (string, error) {
		return `{"profile":"report","rationale":"Only a written analysis was requested","commands":[],"checks":["Source accuracy","Completeness against request"]}`, nil
	}}
	if _, err := p.Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: t.TempDir()}); err == nil {
		t.Fatal("missing report accepted")
	}
	plan, err := p.Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: t.TempDir(), ExecutionOutput: "Findings with sources"})
	if err != nil || len(plan.Commands) != 0 {
		t.Fatalf("report plan=%+v %v", plan, err)
	}
}

func TestAutomaticReportReachesReviewWithoutFakeCommandResults(t *testing.T) {
	f := setupWorkflow(t, nil, 3)
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	f.coordinator.deps.Planner = plannerFunc(func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		return domain.LocalVerificationPlan{Profile: "report", Rationale: "Report only", Checks: []string{"Completeness"}, Commands: []string{}}, nil
	})
	s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "automatic-report", "Produce a report")
	if err != nil {
		t.Fatal(err)
	}
	s = driveAutomaticReady(t, f, s.Run.ID)
	if s.Run.Phase != domain.WorkflowReady || !s.Current || len(s.Tests) != 0 || len(s.Reviews) != 1 {
		t.Fatalf("missing independent content review: %+v", s)
	}
}

type plannerFunc func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error)

func (p plannerFunc) Plan(ctx context.Context, r domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
	return p(ctx, r)
}

type automaticFixtureStore struct {
	*sqlite.Store
	pending []domain.LocalWorkflowIntent
}

func (s *automaticFixtureStore) ListPendingLocalWorkflowIntents(context.Context) ([]domain.LocalWorkflowIntent, error) {
	return append([]domain.LocalWorkflowIntent{}, s.pending...), nil
}
func (s *automaticFixtureStore) ConsumeLocalWorkflowIntent(_ context.Context, id domain.SessionID, request, workflow string, _ time.Time) (bool, error) {
	for i, p := range s.pending {
		if p.SessionID == id && p.RequestID == request {
			s.pending = append(s.pending[:i], s.pending[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func TestAutomaticIntentsStartOnceWithoutResendingExecutor(t *testing.T) {
	f := setupWorkflow(t, []string{"true"}, 3)
	store := &automaticFixtureStore{Store: f.store, pending: []domain.LocalWorkflowIntent{{SessionID: f.session.ID, RequestID: "human-1", Prompt: "Implement approved fixes", TurnID: "turn-1"}}}
	f.coordinator.deps.Store = store
	for i := 0; i < 3; i++ {
		if err := f.coordinator.ProcessAutomaticIntents(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	s, err := f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil || s.Run == nil || !s.Run.Automatic || s.Run.LatestUserPrompt != "Implement approved fixes" || len(store.pending) != 0 {
		t.Fatalf("automatic start failed: %+v %v", s, err)
	}
	if f.worker.fixes != 0 {
		t.Fatal("opening prompt was resent")
	}
	first := s.Run.ID
	// Simulate daemon loss after durable Start, before intent acknowledgement.
	store.pending = []domain.LocalWorkflowIntent{{SessionID: f.session.ID, RequestID: "human-1", Prompt: "Implement approved fixes", TurnID: "turn-1"}}
	if err = f.coordinator.ProcessAutomaticIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil || s.Run.ID != first || len(store.pending) != 0 {
		t.Fatal("restart duplicated a task")
	}
	// A follow-up invalidates the old result immediately and creates a fresh run.
	store.pending = []domain.LocalWorkflowIntent{{SessionID: f.session.ID, RequestID: "human-2", Prompt: "Now implement the report recommendations", TurnID: "turn-2"}}
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil || !s.Automation.Pending || s.Current || s.Accepted {
		t.Fatalf("old readiness survived a new instruction: %+v %v", s, err)
	}
	if err = f.coordinator.ProcessAutomaticIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil || s.Run.ID == first || s.Run.LatestUserPrompt != "Now implement the report recommendations" || f.worker.stopped {
		t.Fatalf("follow-up failed or interrupted human work: %+v %v", s, err)
	}
}

func TestAutomaticPipelineCorrectsFailedTestsAndReviewsAgain(t *testing.T) {
	f := setupWorkflow(t, []string{`test "$(cat value)" = correct`}, 3)
	f.coordinator.deps.Planner = plannerFunc(func(_ context.Context, r domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		return domain.LocalVerificationPlan{Profile: "implementation", Rationale: "Changed behavior", Commands: r.TestCommands, Checks: []string{"Regression tests"}}, nil
	})
	s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "auto-failed-check", "Implement fix")
	if err != nil {
		t.Fatal(err)
	}
	var failedOperation *domain.LocalWorkflowOperation
	var failedResult *domain.LocalWorkflowTestResult
	sawNewCycle, sawFreshReview := false, false
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err = f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
			t.Fatal(err)
		}
		s, err = f.coordinator.Get(t.Context(), f.session.ID)
		if err != nil {
			t.Fatal(err)
		}
		// A fast correction can finish between Evaluate and Get. Its durable
		// failed test remains observable even after the card enters cycle two.
		if candidate := latestOperation(s.Operations, "tests", 1); failedOperation == nil && candidate != nil && candidate.State == "failed" {
			failedOperation = candidate
			for _, result := range s.Tests {
				if result.OperationID == failedOperation.ID {
					capturedResult := result
					failedResult = &capturedResult
					break
				}
			}
		}
		if s.Run.Cycle == 2 {
			sawNewCycle = true
			if s.Run.BlockCode != "" || s.Run.BlockMessage != "" {
				t.Fatalf("previous failure is still presented in the new %s checkpoint: code=%q message=%q", s.Run.Phase, s.Run.BlockCode, s.Run.BlockMessage)
			}
			sawFreshReview = sawFreshReview || s.Run.Phase == domain.WorkflowReviewing
		}
		if s.Run.Phase == domain.WorkflowReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Current || s.Accepted || s.Run.Cycle != 2 || len(s.Tests) != 2 || len(s.Reviews) != 1 || f.worker.fixes != 1 {
		t.Fatalf("automatic fix loop did not complete: %+v fixes=%d", s, f.worker.fixes)
	}
	if !sawNewCycle || !sawFreshReview || failedOperation == nil || failedResult == nil || failedOperation.State != "failed" || failedOperation.ErrorMessage == "" || failedResult.Status != "failed" {
		t.Fatalf("failure/correction/checkpoint evidence was not observed: cycle=%v review=%v operation=%+v result=%+v", sawNewCycle, sawFreshReview, failedOperation, failedResult)
	}
	if !reflect.DeepEqual(latestOperation(s.Operations, "tests", 1), failedOperation) {
		t.Fatal("clearing the current failure changed the historical failed operation")
	}
	historicalResultFound := false
	for _, result := range s.Tests {
		if result.ID == failedResult.ID {
			historicalResultFound = reflect.DeepEqual(result, *failedResult)
		}
	}
	if !historicalResultFound {
		t.Fatal("clearing the current failure changed the historical failed test result")
	}
	if !strings.Contains(f.worker.prompts[0], "Do not weaken/skip checks") {
		t.Fatal("test-failure correction lacks constraints")
	}
}

func TestDiscoveryDoesNotReadEscapedManifests(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "package.json"), []byte(`{"scripts":{"test":"bad"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "frontend")); err != nil {
		t.Fatal(err)
	}
	candidates, err := discoverChecks(root, nil)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("symlink escaped workspace: %v %v", candidates, err)
	}
}

func TestPlannerCannotTreatStaticChecksOrNoOpsAsTests(t *testing.T) {
	for _, command := range []string{"true", "echo passed", "npm run lint", "go vet ./...", "npm run typecheck", "python3 -m ruff check .", "python3 -m mypy ."} {
		t.Run(command, func(t *testing.T) {
			p := ModelPlanner{Generate: func(context.Context, domain.SessionID, string, string) (string, error) {
				b, _ := json.Marshal(domain.LocalVerificationPlan{Profile: "implementation", Rationale: "Implementing", Commands: []string{command}, Checks: []string{"Regression tests"}})
				return string(b), nil
			}}
			if _, err := p.Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: t.TempDir(), TestCommands: []string{command}}); err == nil {
				t.Fatal("non-test command satisfied implementation tests")
			}
		})
	}
}

func TestAutomaticRecoveryUsesFreshVerificationAndRespectsStop(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	planner := plannerFunc(func(_ context.Context, r domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		return domain.LocalVerificationPlan{Profile: "implementation", Rationale: "Behavior", Commands: r.TestCommands, Checks: []string{"regression"}}, nil
	})
	f.coordinator.deps.Planner = planner
	s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "recover-auto", "Implement")
	if err != nil {
		t.Fatal(err)
	}
	s = driveAutomaticReady(t, f, s.Run.ID)
	oldTest := latestOperation(s.Operations, "tests", s.Run.Cycle).ID
	if err = f.coordinator.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.coordinator = New(f.coordinator.deps)
	t.Cleanup(func() { _ = f.coordinator.Close(context.Background()) })
	if err = f.coordinator.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	s = driveAutomaticReady(t, f, s.Run.ID)
	if latestOperation(s.Operations, "tests", s.Run.Cycle).ID == oldTest || !s.Current {
		t.Fatal("restart reused lost observation evidence")
	}
	s, err = f.coordinator.Stop(t.Context(), f.session.ID, s.Run.Version)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
			t.Fatal(err)
		}
	}
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil || s.Run.Phase != domain.WorkflowStopped {
		t.Fatal("automatic recovery undid explicit Stop")
	}
}

func driveAutomaticReady(t *testing.T, f *workflowFixture, id string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.coordinator.Evaluate(t.Context(), id); err != nil {
			t.Fatal(err)
		}
		s, err := f.coordinator.Get(t.Context(), f.session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if s.Run.Phase == domain.WorkflowReady {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, _ := f.coordinator.Get(t.Context(), f.session.ID)
	t.Fatalf("workflow did not reach readiness: %+v", s.Run)
	return Snapshot{}
}

type drainingWorker struct {
	Worker
	attempts   int
	deliveries int
	queued     bool
}

func (w *drainingWorker) Drain(context.Context, domain.SessionID) error {
	w.attempts++
	if w.attempts == 1 {
		return fmt.Errorf("temporary controller reconnect")
	}
	if w.queued {
		w.deliveries++
		w.queued = false
	}
	return nil
}
func (w *drainingWorker) Observe(ctx context.Context, id domain.SessionID, operation string) (WorkerObservation, error) {
	obs, err := w.Worker.Observe(ctx, id, operation)
	if w.queued {
		obs.State = "queued"
	}
	return obs, err
}

func TestAutomaticIntentRetriesDrainAfterDurableStart(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	store := &automaticFixtureStore{Store: f.store, pending: []domain.LocalWorkflowIntent{{SessionID: f.session.ID, RequestID: "queued-human", Prompt: "Implement", TurnID: "turn"}}}
	worker := &drainingWorker{Worker: f.worker, queued: true}
	f.coordinator.deps.Store = store
	f.coordinator.deps.Worker = worker
	if err := f.coordinator.ProcessAutomaticIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	first, err := f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil || first.Run == nil || len(store.pending) != 1 {
		t.Fatal("failed drain lost its durable intent")
	}
	if err = f.coordinator.ProcessAutomaticIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil || current.Run.ID != first.Run.ID || len(store.pending) != 0 || worker.deliveries != 1 {
		t.Fatalf("replay lost or duplicated input: %+v, deliveries %d, %v", current, worker.deliveries, err)
	}
	// Even a consumed request recovers a queued turn after controller reconnect.
	worker.queued = true
	if err = f.coordinator.Evaluate(t.Context(), current.Run.ID); err != nil {
		t.Fatal(err)
	}
	if worker.queued || worker.deliveries != 2 {
		t.Fatal("periodic evaluation did not resume queued work")
	}
}

func TestDiscoveryFindsPythonChecksInNamedBackend(t *testing.T) {
	root := t.TempDir()
	backend := filepath.Join(root, "backend-py")
	if err := os.Mkdir(backend, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backend, "pyproject.toml"), []byte("[project]\nname='example'\n[project.optional-dependencies]\ndev=['pytest>=8', 'ruff>=0.9']\n[tool.pytest.ini_options]\ntestpaths=['tests']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	checks, err := discoverChecks(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range checks {
		if c.Command == "cd backend-py && python3 -m pytest" && c.Kind == "test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Python backend not discovered: %+v", checks)
	}
}

func TestSuccessfulTestsClearPreviousBlockBeforeReview(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	s, err := f.coordinator.Start(t.Context(), f.session.ID, "clear-prior-test-error")
	if err != nil {
		t.Fatal(err)
	}
	// A checkpoint persisted by an older daemon can retain an earlier failure.
	// Fresh successful test evidence must supersede it without deleting history.
	run := *s.Run
	run.BlockCode = "tests_failed"
	run.BlockMessage = "previous test command failed"
	changed, err := f.store.AdvanceLocalWorkflow(t.Context(), run.Version, run, nil)
	if err != nil || !changed {
		t.Fatalf("seed previous failure: changed=%v err=%v", changed, err)
	}
	s = awaitPhase(t, f, domain.WorkflowReviewing)
	if s.Run.BlockCode != "" || s.Run.BlockMessage != "" {
		t.Fatalf("successful tests retained obsolete failure: %+v", s.Run)
	}
	tests := latestOperation(s.Operations, "tests", s.Run.Cycle)
	if tests == nil || tests.State != "succeeded" || len(s.Tests) != 1 || s.Tests[0].Status != "succeeded" {
		t.Fatalf("review checkpoint has no successful test evidence: %+v", s)
	}
}
