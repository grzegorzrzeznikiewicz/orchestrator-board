// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localsnapshot"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// Removing the daemon history handoff must make a factual report impossible to
// verify even when its executor correctly mentions earlier tests and review.
func TestReportReviewReceivesBoundedDaemonFactsFromEarlierWorkflow(t *testing.T) {
	e, store, launcher, worker := localEngine(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	e.clock = func() time.Time { return now }
	manifest, err := localsnapshot.Capture(ctx, worker.Metadata.WorkspacePath)
	if err != nil {
		t.Fatal(err)
	}
	seedEvidenceHistory(t, store, worker, e.localDataDir, "implementation-run", manifest.ID, now.Add(-time.Hour))
	other := worker
	other, err = store.CreateSession(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	seedEvidenceHistory(t, store, other, e.localDataDir, "other-session-run", "other-snapshot", now.Add(-time.Minute))

	report := seedEvidenceReport(t, store, worker, now)
	// beginReview's caller owns an earlier checkpoint. Evidence must be freshly
	// read from the daemon, rather than calling the reviewing phase ready.
	report.Phase = domain.WorkflowTesting
	report.ExecutionOutput = "Earlier implementation had 46 tests and 7 check files pass, and Claude approved."
	result, err := e.TriggerWithOptions(ctx, worker.ID, TriggerOptions{WorkflowRun: &report, WorkflowOperationID: "report-review-op", ExpectedSnapshotID: manifest.ID, Rerun: true})
	if err != nil {
		t.Fatal(err)
	}
	prompt, system := reviewTexts(launcher.gotSpec)
	start := strings.Index(prompt, "<daemon-evidence>\n")
	end := strings.Index(prompt, "\n</daemon-evidence>")
	if start < 0 || end <= start {
		t.Fatalf("report reviewer lacks daemon-provided provenance: %s", prompt)
	}
	var evidence map[string]any
	if err := json.Unmarshal([]byte(prompt[start+len("<daemon-evidence>\n"):end]), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence["source"] != "daemon" || evidence["capturedAt"] != "2026-10-20T10:00:00Z" || evidence["reviewRunId"] != result.Run.ID || evidence["targetSnapshotId"] != manifest.ID {
		t.Fatalf("missing launch provenance: %+v", evidence)
	}
	raw := prompt[start:end]
	for _, want := range []string{`"workflowId":"implementation-run"`, `"historical":true`, `"profile":"implementation"`, `"phase":"reviewing"`, `"workflowOperationId":"implementation-run-review-op"`, `"verdict":"approved"`, `"passed":46`, `"passed":7`, `"commandIndex":0`, `"exitCode":0`} {
		if !strings.Contains(raw, want) {
			t.Errorf("daemon evidence missing %s: %s", want, raw)
		}
	}
	for _, secret := range []string{"other-session-run", "private-token", "PRIVATE_KEY", "--token", "raw executor chat", "private review body", "/private/source"} {
		if strings.Contains(raw, secret) {
			t.Errorf("private or unrelated history leaked %q: %s", secret, raw)
		}
	}
	if strings.Contains(system, "Inspect only the captured snapshot,") || !strings.Contains(system, "Do not run project programs, tests") {
		t.Fatalf("reviewer cannot use supplied provenance safely: %s", system)
	}
	for _, workflow := range launcher.gotSpec.DaemonEvidence.Workflows {
		for _, review := range workflow.Reviews {
			if review.ReviewRunID == result.Run.ID {
				t.Fatal("pending report review was offered as prior approval")
			}
		}
	}
	if !strings.Contains(prompt, "Historical records do not establish current readiness") || !strings.Contains(prompt, "Missing necessary evidence is an error") {
		t.Fatalf("unsafe report evidence instructions: %s", prompt)
	}
}

func seedEvidenceHistory(t *testing.T, store *sqlite.Store, worker domain.SessionRecord, dataDir, id, snapshot string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	run, _, err := store.CreateLocalWorkflow(ctx, domain.LocalWorkflowRun{ID: id, SessionID: worker.ID, Phase: domain.WorkflowTesting, Cycle: 1, MaxRounds: 3, Task: "raw executor chat", Env: map[string]string{"PRIVATE_KEY": "private-token"}, WorkspacePath: "/private/source", ExecutionOutput: "raw executor chat", VerificationPlan: &domain.LocalVerificationPlan{Profile: "implementation", Rationale: "code", Commands: []string{"tool --token private-token"}, Checks: []string{"works"}}, CreatedAt: at, UpdatedAt: at}, id+"-request")
	if err != nil {
		t.Fatal(err)
	}
	op := domain.LocalWorkflowOperation{ID: id + "-test-op", WorkflowID: id, Kind: "tests", State: "reserved", Cycle: 1, Attempt: 1, SnapshotID: snapshot, StartedAt: at}
	if ok, err := store.AdvanceLocalWorkflow(ctx, run.Version, run, &op); err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	run.Version++
	logPath := filepath.Join(dataDir, id+".stdout")
	if err := os.WriteFile(logPath, []byte("PRIVATE_KEY=private-token\nTests 46 passed (46)\nTest Files 7 passed (7)\nraw executor chat --token private-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code := 0
	op.State, op.CompletedAt = "succeeded", &at
	run.Phase = domain.WorkflowReady
	test := domain.LocalWorkflowTestResult{ID: id + "-test", OperationID: op.ID, CommandIndex: 0, Command: "tool --token private-token", Status: "succeeded", ExitCode: &code, StdoutLog: logPath, StartedAt: at, CompletedAt: &at}
	if ok, err := store.CompleteLocalWorkflowOperation(ctx, run.Version, op, run, []domain.LocalWorkflowTestResult{test}); err != nil || !ok {
		t.Fatalf("tests: %v %v", ok, err)
	}
	run.Version++
	op = domain.LocalWorkflowOperation{ID: id + "-review-op", WorkflowID: id, Kind: "review", State: "reserved", Cycle: 1, Attempt: 1, SnapshotID: snapshot, StartedAt: at}
	if ok, err := store.AdvanceLocalWorkflow(ctx, run.Version, run, &op); err != nil || !ok {
		t.Fatalf("review: %v %v", ok, err)
	}
	run.Version++
	reviewID := id + "-review"
	if err := store.UpsertReview(ctx, domain.Review{ID: reviewID, SessionID: worker.ID, ProjectID: worker.ProjectID, Harness: domain.ReviewerClaudeCode, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertReviewRun(ctx, domain.ReviewRun{ID: id + "-review-run", ReviewID: reviewID, SessionID: worker.ID, WorkflowOperationID: op.ID, Harness: domain.ReviewerClaudeCode, TargetKind: domain.ReviewTargetLocal, SnapshotID: snapshot, SnapshotPath: "/historical/snapshot", Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, Body: "private review body", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	op.State, op.CompletedAt, op.ReviewRunID = "succeeded", &at, id+"-review-run"
	run.ClosedAt = &at
	if ok, err := store.CompleteLocalWorkflowOperation(ctx, run.Version, op, run, nil); err != nil || !ok {
		t.Fatalf("close: %v %v", ok, err)
	}
}

func seedEvidenceReport(t *testing.T, store *sqlite.Store, worker domain.SessionRecord, at time.Time) domain.LocalWorkflowRun {
	t.Helper()
	ctx := context.Background()
	run, _, err := store.CreateLocalWorkflow(ctx, domain.LocalWorkflowRun{ID: "report-run", SessionID: worker.ID, Phase: domain.WorkflowReviewing, Cycle: 1, MaxRounds: 3, WorkerGeneration: domain.LocalWorkflowWorkerGeneration(worker), Task: "Report earlier verification", VerificationPlan: &domain.LocalVerificationPlan{Profile: "report", Rationale: "status report", Checks: []string{"check historical facts"}}, CreatedAt: at, UpdatedAt: at}, "report-request")
	if err != nil {
		t.Fatal(err)
	}
	op := domain.LocalWorkflowOperation{ID: "report-review-op", WorkflowID: run.ID, Kind: "review", Cycle: 1, Attempt: 1, State: "reserved", StartedAt: at}
	if ok, err := store.AdvanceLocalWorkflow(ctx, run.Version, run, &op); err != nil || !ok {
		t.Fatalf("reserve report: %v %v", ok, err)
	}
	return run
}

func TestReportVerificationContextOmitsCommandsAndRedactsCredentialValues(t *testing.T) {
	prompt, _ := reviewTexts(LaunchSpec{TargetKind: domain.ReviewTargetLocal, ProjectEnv: map[string]string{"PRIVATE_KEY": "configured-credential"}, VerificationPlan: &domain.LocalVerificationPlan{Profile: "report", Rationale: "inspect configured-credential", Commands: []string{"custom-tool --token inline-credential"}, Checks: []string{"check facts"}}, ExecutionOutput: "The custom-tool --token inline-credential used PRIVATE_KEY=configured-credential and api_key=another-credential.\n46 tests passed.\n" + strings.Repeat("report text ", 10000)})
	for _, secret := range []string{"configured-credential", "inline-credential", "another-credential", "custom-tool"} {
		if strings.Contains(prompt, secret) {
			t.Errorf("private verification context leaked %q", secret)
		}
	}
	if len(prompt) > 40000 || !strings.Contains(prompt, `"ExecutorOutputTruncated":true`) || !strings.Contains(prompt, "46 tests passed") {
		t.Fatalf("unbounded or missing report response: %d bytes", len(prompt))
	}
}

type incompleteEvidenceStore struct {
	Store
	ports.LocalReviewWorkspace
	localReviewStore
}

func TestReportReviewFailsClosedWhenDaemonHistoryIsUnavailable(t *testing.T) {
	e, store, launcher, worker := localEngine(t)
	report := seedEvidenceReport(t, store, worker, time.Now().UTC())
	e.store = incompleteEvidenceStore{Store: store, LocalReviewWorkspace: store, localReviewStore: store}
	_, err := e.TriggerWithOptions(context.Background(), worker.ID, TriggerOptions{WorkflowRun: &report, Rerun: true})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "report evidence unavailable") || launcher.spawned {
		t.Fatalf("report reviewed without daemon facts: %v spawned=%v", err, launcher.spawned)
	}
	runs, err := store.ListReviewRunsBySession(context.Background(), worker.ID)
	if err != nil || len(runs) != 1 || runs[0].Status != domain.ReviewRunFailed {
		t.Fatalf("missing durable failure: %+v %v", runs, err)
	}
}

type evidenceFixtureStore struct {
	Store
	current   domain.LocalWorkflowRun
	history   []domain.LocalWorkflowRun
	ops       map[string][]domain.LocalWorkflowOperation
	tests     map[string][]domain.LocalWorkflowTestResult
	decisions map[string][]domain.LocalWorkflowDecision
}

func (s evidenceFixtureStore) CurrentLocalWorkflow(context.Context, domain.SessionID) (domain.LocalWorkflowRun, bool, error) {
	return s.current, true, nil
}
func (s evidenceFixtureStore) ListSessionLocalWorkflows(context.Context, domain.SessionID) ([]domain.LocalWorkflowRun, error) {
	return s.history, nil
}
func (s evidenceFixtureStore) ListLocalWorkflowOperations(_ context.Context, id string) ([]domain.LocalWorkflowOperation, error) {
	return s.ops[id], nil
}
func (s evidenceFixtureStore) ListLocalWorkflowTests(_ context.Context, id string) ([]domain.LocalWorkflowTestResult, error) {
	return s.tests[id], nil
}
func (s evidenceFixtureStore) ListLocalWorkflowDecisions(_ context.Context, id string) ([]domain.LocalWorkflowDecision, error) {
	return s.decisions[id], nil
}

func TestReportEvidenceBoundsHistoryAndExplainsOmissions(t *testing.T) {
	current := domain.LocalWorkflowRun{ID: "current", SessionID: "worker", Phase: domain.WorkflowReviewing}
	store := evidenceFixtureStore{Store: &fakeStore{}, current: current, ops: map[string][]domain.LocalWorkflowOperation{}, tests: map[string][]domain.LocalWorkflowTestResult{}}
	for i := range 20 {
		id := fmt.Sprintf("history-%02d", i)
		store.history = append(store.history, domain.LocalWorkflowRun{ID: id, SessionID: "worker"})
	}
	for i := range 50 {
		id := fmt.Sprintf("operation-%02d", i)
		store.ops["current"] = append(store.ops["current"], domain.LocalWorkflowOperation{ID: id, WorkflowID: "current", Kind: "tests", State: "succeeded"})
		store.tests["current"] = append(store.tests["current"], domain.LocalWorkflowTestResult{ID: fmt.Sprintf("test-%02d", i), OperationID: id})
	}
	e := &Engine{store: store, clock: time.Now}
	evidence, err := e.reportEvidence(context.Background(), current, domain.ReviewRun{ID: "pending", SessionID: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Workflows) != 6 || evidence.OmittedWorkflows != 15 || len(evidence.Workflows[0].Operations) != 24 || evidence.Workflows[0].OmittedOperations != 26 || len(evidence.Workflows[0].Tests) != 24 || evidence.Workflows[0].OmittedTests != 26 {
		t.Fatalf("history not bounded with explicit omissions: %+v", evidence)
	}
	if evidence.Workflows[0].Operations[0].OperationID != "operation-49" || evidence.Workflows[0].Tests[0].TestID != "test-49" {
		t.Fatal("bounds discarded recent evidence")
	}
	encoded, err := json.Marshal(evidence)
	if err != nil || len(encoded) > 64*1024 {
		t.Fatalf("oversize evidence: %d %v", len(encoded), err)
	}
	// A second launch must not reorder or otherwise mutate stored facts.
	again, err := e.reportEvidence(context.Background(), current, domain.ReviewRun{ID: "pending-2", SessionID: "worker"})
	if err != nil || again.Workflows[0].Tests[0].TestID != "test-49" {
		t.Fatalf("collection mutated stored evidence: %+v %v", again, err)
	}
}

func TestReportEvidenceNeverReadsLogsOutsideDaemonRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(outside, []byte("Tests 999 passed (999)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "symlink.stdout")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	e := &Engine{localDataDir: root}
	for _, path := range []string{outside, link} {
		counts, _, unavailable := e.testCounts(domain.LocalWorkflowTestResult{StdoutLog: path})
		if len(counts) != 0 || !unavailable {
			t.Fatalf("read unowned log through %s: %+v unavailable=%v", path, counts, unavailable)
		}
	}
}

func TestReportEvidenceRejectsAnOversizeCurrentEnvelope(t *testing.T) {
	current := domain.LocalWorkflowRun{ID: strings.Repeat("a", 70*1024), SessionID: "worker"}
	e := &Engine{store: evidenceFixtureStore{Store: &fakeStore{}, current: current}, clock: time.Now}
	_, err := e.reportEvidence(context.Background(), current, domain.ReviewRun{ID: "pending", SessionID: "worker"})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "report evidence unavailable") {
		t.Fatalf("oversize evidence was accepted: %v", err)
	}
}

func TestReportEvidenceReadsBoundedNumericCountsWithoutRawOutput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "stdout")
	text := strings.Repeat("private credential value\n", 4000) + "\x1b[32mTests 46 passed (46)\x1b[0m\n" + strings.Repeat("Test Files 7 passed (7)\n", 12)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	e := &Engine{localDataDir: root}
	counts, omitted, unavailable := e.testCounts(domain.LocalWorkflowTestResult{StdoutLog: path})
	if len(counts) != 8 || counts[0].Kind != "tests" || counts[0].Passed != 46 || !omitted || unavailable {
		t.Fatalf("incorrect bounded observed counts: %+v omitted=%v unavailable=%v", counts, omitted, unavailable)
	}
	encoded, err := json.Marshal(counts)
	if err != nil || strings.Contains(string(encoded), "private") {
		t.Fatalf("raw stdout entered evidence: %s %v", encoded, err)
	}
}

func TestReportEvidenceKeepsReviewAndHumanDecisionProvenance(t *testing.T) {
	ctx := context.Background()
	current := domain.LocalWorkflowRun{ID: "current", SessionID: "worker", Phase: domain.WorkflowReviewing}
	reviews := &fakeStore{runs: []domain.ReviewRun{
		{ID: "pending", SessionID: "worker", WorkflowOperationID: "review-op", TargetKind: domain.ReviewTargetLocal, Status: domain.ReviewRunRunning},
		{ID: "foreign-review", SessionID: "other-session", WorkflowOperationID: "review-op", TargetKind: domain.ReviewTargetLocal, Verdict: domain.VerdictApproved},
		{ID: "real-prior-review", SessionID: "worker", WorkflowOperationID: "review-op", TargetKind: domain.ReviewTargetLocal, SnapshotID: "reviewed-content", Verdict: domain.VerdictChangesRequested, Status: domain.ReviewRunComplete},
	}}
	store := evidenceFixtureStore{Store: reviews, current: current, ops: map[string][]domain.LocalWorkflowOperation{"current": {{ID: "review-op", WorkflowID: "current", Kind: "review", State: "reserved"}}}, decisions: map[string][]domain.LocalWorkflowDecision{"current": {
		{ID: "desktop-decision", WorkflowID: "current", Kind: "accepted", Source: "desktop_user", SnapshotID: "accepted-content", TestOperationID: "tested-op", ReviewRunID: "accepted-review", Comment: "password=do-not-include"},
		{ID: "executor-decision", WorkflowID: "current", Kind: "accepted", Source: "executor"},
	}}}
	e := &Engine{store: store, clock: time.Now}
	evidence, err := e.reportEvidence(ctx, current, domain.ReviewRun{ID: "pending", SessionID: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	facts := evidence.Workflows[0]
	if len(facts.Reviews) != 1 || facts.Reviews[0].ReviewRunID != "real-prior-review" || facts.Reviews[0].Verdict != domain.VerdictChangesRequested || facts.Reviews[0].SnapshotID != "reviewed-content" {
		t.Fatalf("pending or foreign approval replaced recorded verdict: %+v", facts.Reviews)
	}
	if len(facts.Decisions) != 1 || facts.Decisions[0].DecisionID != "desktop-decision" || facts.Decisions[0].SnapshotID != "accepted-content" || facts.Decisions[0].ReviewRunID != "accepted-review" || facts.Decisions[0].TestOperationID != "tested-op" {
		t.Fatalf("unproven user acceptance: %+v", facts.Decisions)
	}
	encoded, err := json.Marshal(evidence)
	if err != nil || strings.Contains(string(encoded), "do-not-include") {
		t.Fatalf("private human comment leaked: %s %v", encoded, err)
	}
}

// These are the pytest summaries observed in the Trains Watch implementation
// workflow. Losing them leaves the report's factual test totals unverifiable.
func TestReportEvidenceRecognizesPytestNumericSummaries(t *testing.T) {
	for _, tc := range []struct {
		name, output    string
		passed, skipped int
	}{
		{"complete suite", "======================== 46 passed, 1 warning in 1.49s =========================\n", 46, 0},
		{"environment omissions", "=================== 43 passed, 3 skipped, 1 warning in 0.85s ===================\n", 43, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "pytest.stdout")
			if err := os.WriteFile(path, []byte(tc.output+"private-token and raw diagnostic details\n"), 0600); err != nil {
				t.Fatal(err)
			}
			e := &Engine{localDataDir: root}
			counts, omitted, unavailable := e.testCounts(domain.LocalWorkflowTestResult{StdoutLog: path})
			if len(counts) != 1 || counts[0].Kind != "tests" || counts[0].Stream != "stdout" || counts[0].Passed != tc.passed || counts[0].Skipped != tc.skipped || counts[0].Failed != 0 || omitted || unavailable {
				t.Fatalf("missing observed pytest totals: %+v omitted=%v unavailable=%v", counts, omitted, unavailable)
			}
			encoded, err := json.Marshal(counts)
			if err != nil || strings.Contains(string(encoded), "private-token") || strings.Contains(string(encoded), "warning") || strings.Contains(string(encoded), "1.49") {
				t.Fatalf("raw pytest output leaked: %s %v", encoded, err)
			}
		})
	}
}

func TestReportEvidenceMarksUnrecognizedOrPoisonedSummariesUnavailable(t *testing.T) {
	for _, output := range []string{
		"46 passed, 1 warning in 1.49s private-token\n",
		"=================== 43 passed, 3 skipped, private-token in 0.85s ===================\n",
		"No recognized test summary was captured.\n",
		"",
	} {
		root := t.TempDir()
		path := filepath.Join(root, "pytest.stdout")
		if err := os.WriteFile(path, []byte(output), 0600); err != nil {
			t.Fatal(err)
		}
		e := &Engine{localDataDir: root}
		counts, _, unavailable := e.testCounts(domain.LocalWorkflowTestResult{StdoutLog: path})
		if len(counts) != 0 || !unavailable {
			t.Fatalf("unrecognized or poisoned output treated as evidence: %+v unavailable=%v", counts, unavailable)
		}
	}
}
