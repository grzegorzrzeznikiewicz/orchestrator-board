package controllers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	reviewsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// Only the model and native confirmation are doubles. HTTP, test processes,
// snapshot guards, reviewer validation and persistence use production code.
type workflowRegression struct {
	root, data, path string
	session          domain.SessionRecord
	store            *sqlite.Store
	model            *workflowModel
	engine           *reviewcore.Engine
	coordinator      *localworkflow.Coordinator
	server           *httptest.Server
}

type shortWorkflowTests struct{ *localworkflow.FileTestRunner }

func (r shortWorkflowTests) Run(ctx context.Context, plan localworkflow.TestPlan) ([]domain.LocalWorkflowTestResult, error) {
	plan.CommandTimeout = 300 * time.Millisecond
	return r.FileTestRunner.Run(ctx, plan)
}

func newWorkflowRegression(t *testing.T, command string) *workflowRegression {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test command fixtures; Windows native checks run separately")
	}
	f := &workflowRegression{root: t.TempDir(), data: t.TempDir()}
	if out, err := exec.Command("git", "init", "--quiet", f.root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = sqlitetest.Open(f.data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.close)
	ctx := context.Background()
	cfg := domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal, TestCommands: []string{command}, Worker: domain.RoleOverride{Harness: domain.HarnessCodex}, Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerClaudeCode}}}
	if err = f.store.UpsertProject(ctx, domain.ProjectRecord{ID: "regression", Path: f.root, Config: cfg, RegisteredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	f.session, err = f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "regression", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: time.Now()}, Metadata: domain.SessionMetadata{WorkspacePath: f.root, Prompt: "Value must be correct and tests must pass", ControllerGeneration: "gen"}, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	f.path = "/api/v1/sessions/" + string(f.session.ID)
	f.model = &workflowModel{root: f.root}
	f.openServices()
	f.request(t, http.MethodPost, "/local-workflow/start", map[string]any{"requestId": "regression-start"}, 200)
	return f
}

func (f *workflowRegression) openServices() {
	f.engine = reviewcore.New(reviewcore.Deps{Store: f.store, Sessions: f.store, PRs: f.store, Projects: f.store, Launcher: &localReviewLauncher{}, DataDir: f.data})
	reviews := reviewsvc.New(f.engine, f.store)
	f.coordinator = localworkflow.New(localworkflow.Deps{Decisions: &workflowDesktopConfirmer{allow: true}, Store: f.store, Worker: f.model, Tests: shortWorkflowTests{localworkflow.NewTestRunner(f.data)}, Reviews: reviews, DataDir: f.data})
	f.server = httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, httpd.APIDeps{Reviews: reviews, LocalWorkflow: f.coordinator}, httpd.ControlDeps{}))
}

func (f *workflowRegression) close() {
	if f.server != nil {
		f.server.Close()
	}
	if f.coordinator != nil {
		_ = f.coordinator.Close(context.Background())
	}
	if f.engine != nil {
		f.engine.CloseLocalReviews()
	}
	if f.store != nil {
		_ = f.store.Close()
	}
}

func (f *workflowRegression) request(t *testing.T, method, suffix string, input any, want int) []byte {
	t.Helper()
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, status, _ := doRequest(t, f.server, method, f.path+suffix, string(body))
	if status != want {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, suffix, status, want, raw)
	}
	return raw
}

func (f *workflowRegression) snapshot(t *testing.T) localworkflow.Snapshot {
	t.Helper()
	var snap localworkflow.Snapshot
	if err := json.Unmarshal(f.request(t, http.MethodGet, "/local-workflow", nil, 200), &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

func (f *workflowRegression) await(t *testing.T, condition func(localworkflow.Snapshot) bool) localworkflow.Snapshot {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		snap := f.snapshot(t)
		if condition(snap) {
			return snap
		}
		if err := f.coordinator.Evaluate(context.Background(), snap.Run.ID); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("workflow did not reach expected state: %+v", f.snapshot(t))
	return localworkflow.Snapshot{}
}

func (f *workflowRegression) runningReview(t *testing.T) domain.ReviewRun {
	t.Helper()
	var result domain.ReviewRun
	f.await(t, func(s localworkflow.Snapshot) bool {
		for _, op := range s.Operations {
			if op.Kind != "review" || op.ReviewRunID == "" {
				continue
			}
			run, found, err := f.store.GetReviewRun(context.Background(), op.ReviewRunID)
			if err != nil {
				t.Fatal(err)
			}
			if found && run.Status == domain.ReviewRunRunning {
				result = run
				return true
			}
		}
		return false
	})
	return result
}

func approvedRegressionResult(run domain.ReviewRun) domain.LocalReviewResult {
	return domain.LocalReviewResult{Version: 1, Round: run.Round, SnapshotID: run.SnapshotID, Outcome: "approved", Summary: "Task and criteria checked", Findings: []domain.LocalReviewFinding{}}
}

func regressionDecision(s localworkflow.Snapshot) map[string]any {
	body := map[string]any{"requestId": "regression-decision", "intentId": "controlled-desktop", "workflowId": s.Run.ID, "expectedVersion": s.Run.Version, "kind": "accepted", "snapshotId": "missing", "testOperationId": "missing", "reviewRunId": "missing"}
	for _, op := range s.Operations {
		if op.Cycle != s.Run.Cycle {
			continue
		}
		if op.Kind == "tests" {
			body["snapshotId"], body["testOperationId"] = op.SnapshotID, op.ID
		}
		if op.Kind == "review" {
			body["reviewRunId"] = op.ReviewRunID
		}
	}
	return body
}

func TestLocalWorkflowHTTPFailedTestsRemainFailedAfterReopen(t *testing.T) {
	for _, tc := range []struct{ name, command, status string }{{"failed", "printf test-diagnostic >&2; exit 7", "failed"}, {"timeout", "sleep 10", "timed_out"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWorkflowRegression(t, tc.command)
			snap := f.await(t, func(s localworkflow.Snapshot) bool { return s.Run.Phase == domain.WorkflowBlocked })
			if snap.Run.BlockCode != "tests_failed" || snap.Accepted || snap.Current || len(snap.Tests) != 1 || snap.Tests[0].Status != tc.status || len(snap.Reviews) != 0 || len(snap.Decisions) != 0 {
				t.Fatalf("failed test advanced workflow: %+v", snap)
			}
			if tc.status == "failed" {
				if snap.Tests[0].ExitCode == nil || *snap.Tests[0].ExitCode != 7 {
					t.Fatal("test exit code lost")
				}
				log := f.request(t, http.MethodGet, "/local-workflow/tests/"+snap.Tests[0].ID+"/logs?stream=stderr", nil, 200)
				if !strings.Contains(string(log), "test-diagnostic") {
					t.Fatalf("diagnostic lost: %s", log)
				}
			}
			f.request(t, http.MethodPost, "/local-workflow/decision", regressionDecision(snap), 409)
			f.close()
			var err error
			f.store, err = sqlite.Open(f.data)
			if err != nil {
				t.Fatal(err)
			}
			f.openServices()
			if err = f.coordinator.Recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			restored := f.snapshot(t)
			if restored.Accepted || restored.Current || len(restored.Tests) != 1 || restored.Tests[0].ID != snap.Tests[0].ID || restored.Tests[0].Status != tc.status || len(restored.Reviews) != 0 || len(restored.Decisions) != 0 {
				t.Fatalf("restart revived failed test: %+v", restored)
			}
			f.model.mu.Lock()
			fixes := f.model.fixes
			f.model.mu.Unlock()
			if fixes != 0 {
				t.Fatal("test failure dispatched model correction")
			}
		})
	}
}

func TestLocalWorkflowHTTPInvalidReviewerCannotAuthorizeAcceptance(t *testing.T) {
	for _, variant := range []string{"missing", "foreign snapshot", "unknown version", "required finding with approval", "model error"} {
		t.Run(variant, func(t *testing.T) {
			f := newWorkflowRegression(t, "test -s value")
			run := f.runningReview(t)
			result := approvedRegressionResult(run)
			body := map[string]any{"runId": run.ID, "localResult": result}
			want := 422
			switch variant {
			case "missing":
				delete(body, "localResult")
			case "foreign snapshot":
				result.SnapshotID = "other-workspace"
			case "unknown version":
				result.Version = 99
			case "required finding with approval":
				result.Findings = []domain.LocalReviewFinding{{Kind: "required", Path: "value", Line: 1, Reason: "Must change"}}
			case "model error":
				result.Outcome = "error"
				want = 200
			}
			if variant != "missing" {
				body["localResult"] = result
			}
			f.request(t, http.MethodPost, "/reviews/submit", body, want)
			blocked := f.await(t, func(s localworkflow.Snapshot) bool { return s.Run.Phase == domain.WorkflowBlocked })
			if blocked.Accepted || blocked.Current || len(blocked.Decisions) != 0 || blocked.Run.BlockCode != "review_error" {
				t.Fatalf("invalid result became ready: %+v", blocked)
			}
			f.request(t, http.MethodPost, "/local-workflow/decision", regressionDecision(blocked), 409)
			// A valid payload cannot rewrite the failed review in place. Explicit
			// Resume must create fresh tests and a new reviewer run first.
			f.request(t, http.MethodPost, "/reviews/submit", map[string]any{"runId": run.ID, "localResult": approvedRegressionResult(run)}, 409)
			f.request(t, http.MethodPost, "/local-workflow/resume", map[string]any{"expectedVersion": blocked.Run.Version}, 200)
			fresh := f.runningReview(t)
			if fresh.ID == run.ID || fresh.Round <= run.Round {
				t.Fatal("resume reused invalid reviewer result")
			}
			f.request(t, http.MethodPost, "/reviews/submit", map[string]any{"runId": fresh.ID, "localResult": approvedRegressionResult(fresh)}, 200)
			ready := f.await(t, func(s localworkflow.Snapshot) bool { return s.Run.Phase == domain.WorkflowReady })
			if !ready.Current || ready.Accepted || len(ready.Tests) != 2 || len(ready.Reviews) != 2 {
				t.Fatalf("fresh evidence not required: %+v", ready)
			}
		})
	}
}

func TestLocalWorkflowHTTPChangedNewFilesInvalidateAcceptedDecision(t *testing.T) {
	f := newWorkflowRegression(t, "test -s value")
	run := f.runningReview(t)
	f.request(t, http.MethodPost, "/reviews/submit", map[string]any{"runId": run.ID, "localResult": approvedRegressionResult(run)}, 200)
	ready := f.await(t, func(s localworkflow.Snapshot) bool { return s.Run.Phase == domain.WorkflowReady })
	body := regressionDecision(ready)
	f.request(t, http.MethodPost, "/local-workflow/decision", body, 200)
	accepted := f.snapshot(t)
	if !accepted.Accepted || len(accepted.Decisions) != 1 {
		t.Fatalf("decision missing: %+v", accepted)
	}
	if err := os.WriteFile(filepath.Join(f.root, "new-untracked.txt"), []byte("new local change"), 0600); err != nil {
		t.Fatal(err)
	}
	stale := f.snapshot(t)
	if stale.Accepted || stale.Current || len(stale.Decisions) != 1 || stale.Decisions[0].ID != accepted.Decisions[0].ID {
		t.Fatalf("new file retained acceptance or erased history: %+v", stale)
	}
	f.request(t, http.MethodPost, "/local-workflow/decision", body, 200)
	retried := f.snapshot(t)
	if retried.Accepted || len(retried.Decisions) != 1 || retried.Decisions[0].ID != accepted.Decisions[0].ID {
		t.Fatalf("retry approved unreviewed file: %+v", retried)
	}
	if exec.Command("git", "-C", f.root, "rev-parse", "--verify", "HEAD").Run() == nil {
		t.Fatal("local acceptance created a commit")
	}
}
