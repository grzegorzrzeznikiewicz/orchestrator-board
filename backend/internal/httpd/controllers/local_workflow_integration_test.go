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
	"sync"
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

func TestMain(m *testing.M) {
	if handled, code := localworkflow.RunTestHost(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

type workflowModel struct {
	mu    sync.Mutex
	root  string
	fixes int
}

func (w *workflowModel) Observe(_ context.Context, _ domain.SessionID, id string) (localworkflow.WorkerObservation, error) {
	state := "idle"
	if id != "" {
		state = "completed"
	}
	return localworkflow.WorkerObservation{State: state, Generation: "chat:gen", TurnID: id}, nil
}
func (w *workflowModel) DeliverFix(ctx context.Context, id domain.SessionID, operationID, text string) (localworkflow.WorkerObservation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fixes++
	if e := os.WriteFile(filepath.Join(w.root, "value"), []byte("correct"), 0600); e != nil {
		return localworkflow.WorkerObservation{}, e
	}
	return w.Observe(ctx, id, operationID)
}
func (w *workflowModel) Interrupt(context.Context, domain.SessionID, string) error { return nil }

type workflowDesktopConfirmer struct{ allow bool }

func (c *workflowDesktopConfirmer) Confirm(context.Context, localworkflow.DecisionChallenge) error {
	if !c.allow {
		return localworkflow.ErrDecisionDenied
	}
	return nil
}

func TestLocalWorkflowHTTPRunsTestsCorrectionsAndRealReviewService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX command fixture; Windows runner is build checked")
	}
	ctx := context.Background()
	root := t.TempDir()
	if out, e := exec.Command("git", "init", "--quiet", root).CombinedOutput(); e != nil {
		t.Fatalf("git init: %s %v", out, e)
	}
	if e := os.WriteFile(filepath.Join(root, "value"), []byte("wrong"), 0600); e != nil {
		t.Fatal(e)
	}
	dataDir := t.TempDir()
	store, e := sqlitetest.Open(dataDir)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = store.Close() }()
	cfg := domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal, TestCommands: []string{"test -s value"}, Worker: domain.RoleOverride{Harness: domain.HarnessCodex}, Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerClaudeCode}}}
	if e = store.UpsertProject(ctx, domain.ProjectRecord{ID: "integration", Path: root, Config: cfg, RegisteredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	worker, e := store.CreateSession(ctx, domain.SessionRecord{ProjectID: "integration", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: time.Now()}, Metadata: domain.SessionMetadata{WorkspacePath: root, Prompt: "Write correct into value. Acceptance criteria: the value is correct and tests pass.", ControllerGeneration: "gen"}, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	engine := reviewcore.New(reviewcore.Deps{Store: store, Sessions: store, PRs: store, Projects: store, Launcher: &localReviewLauncher{}, DataDir: dataDir})
	defer engine.CloseLocalReviews()
	reviews := reviewsvc.New(engine, store)
	model := &workflowModel{root: root}
	desktop := &workflowDesktopConfirmer{}
	coordinator := localworkflow.New(localworkflow.Deps{Decisions: desktop, Store: store, Worker: model, Tests: localworkflow.NewTestRunner(dataDir), Reviews: reviews, DataDir: dataDir})
	defer func() { _ = coordinator.Close(ctx) }()
	router := httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, httpd.APIDeps{Reviews: reviews, LocalWorkflow: coordinator}, httpd.ControlDeps{})
	srv := httptest.NewServer(router)
	defer srv.Close()
	path := "/api/v1/sessions/" + string(worker.ID)
	raw, status, _ := doRequest(t, srv, http.MethodPost, path+"/local-workflow/start", `{"requestId":"integration-start"}`)
	if status != 200 {
		t.Fatalf("start %d: %s", status, raw)
	}
	var first localworkflow.Snapshot
	if e = json.Unmarshal(raw, &first); e != nil {
		t.Fatal(e)
	}
	submitted := map[string]bool{}
	deadline := time.Now().Add(20 * time.Second)
	var final localworkflow.Snapshot
	for time.Now().Before(deadline) {
		if e = coordinator.Evaluate(ctx, first.Run.ID); e != nil {
			t.Fatal(e)
		}
		raw, status, _ = doRequest(t, srv, http.MethodGet, path+"/local-workflow", "")
		if status != 200 {
			t.Fatalf("read %d: %s", status, raw)
		}
		var snap localworkflow.Snapshot
		if e = json.Unmarshal(raw, &snap); e != nil {
			t.Fatal(e)
		}
		if snap.Run.Phase == domain.WorkflowBlocked {
			t.Fatalf("unexpected block: %s", raw)
		}
		if snap.Run.Phase == domain.WorkflowReady {
			final = snap
			break
		}
		for _, op := range snap.Operations {
			if op.Kind != "review" || op.ReviewRunID == "" || submitted[op.ID] {
				continue
			}
			review, ok, e := store.GetReviewRun(ctx, op.ReviewRunID)
			if e != nil || !ok {
				t.Fatalf("review missing: %v", e)
			}
			outcome := "approved"
			findings := []domain.LocalReviewFinding{}
			if op.Cycle == 1 {
				outcome = "changes_requested"
				findings = []domain.LocalReviewFinding{{Kind: "required", Path: "value", Line: 1, Reason: "Value must be correct"}}
			}
			body, e := json.Marshal(map[string]any{"runId": review.ID, "localResult": domain.LocalReviewResult{Version: 1, Round: review.Round, SnapshotID: review.SnapshotID, Outcome: outcome, Summary: "Task and criteria checked", Findings: findings}})
			if e != nil {
				t.Fatal(e)
			}
			response, code, _ := doRequest(t, srv, http.MethodPost, path+"/reviews/submit", string(body))
			if code != 200 {
				t.Fatalf("submit %d: %s", code, response)
			}
			submitted[op.ID] = true
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final.Run == nil || !final.Current || final.Stage != "awaiting_acceptance" || len(final.Reviews) != 2 || final.Reviews[0].LocalResult.Outcome != "changes_requested" || final.Reviews[1].LocalResult.Outcome != "approved" || final.Run.Cycle != 2 || len(final.Tests) != 2 || len(submitted) != 2 {
		t.Fatalf("incomplete loop: %+v", final)
	}
	model.mu.Lock()
	fixes := model.fixes
	model.mu.Unlock()
	if fixes != 1 {
		t.Fatalf("fixes=%d", fixes)
	}
	prs, e := store.ListPRsBySession(ctx, worker.ID)
	if e != nil || len(prs) != 0 {
		t.Fatal("workflow created a PR")
	}
	if e = exec.Command("git", "-C", root, "rev-parse", "--verify", "HEAD").Run(); e == nil {
		t.Fatal("workflow created a commit")
	}
	var testID, reviewID, snapshotID string
	for _, op := range final.Operations {
		if op.Cycle == final.Run.Cycle {
			if op.Kind == "tests" {
				testID = op.ID
				snapshotID = op.SnapshotID
			}
			if op.Kind == "review" {
				reviewID = op.ReviewRunID
			}
		}
	}
	decisionBody, e := json.Marshal(map[string]any{"requestId": "integration-decision", "intentId": "private-desktop-intent", "workflowId": final.Run.ID, "expectedVersion": final.Run.Version, "kind": "accepted", "comment": "Manually checked", "snapshotId": snapshotID, "testOperationId": testID, "reviewRunId": reviewID})
	if e != nil {
		t.Fatal(e)
	}
	_, status, _ = doRequest(t, srv, http.MethodPost, path+"/local-workflow/decision", string(decisionBody))
	if status != 403 {
		t.Fatalf("unconfirmed decision status=%d", status)
	}
	desktop.allow = true
	raw, status, _ = doRequest(t, srv, http.MethodPost, path+"/local-workflow/decision", string(decisionBody))
	if status != 200 {
		t.Fatalf("confirmed decision %d: %s", status, raw)
	}
	var accepted localworkflow.Snapshot
	if e = json.Unmarshal(raw, &accepted); e != nil || !accepted.Accepted || accepted.Stage != "accepted" || len(accepted.Decisions) != 1 {
		t.Fatalf("human decision lost: %+v %v", accepted, e)
	}
	// Reopen storage and services as a new daemon would. Reviewer evidence must
	// come from SQLite, and a lost live guard must not imply readiness.
	if e = coordinator.Close(ctx); e != nil {
		t.Fatal(e)
	}
	engine.CloseLocalReviews()
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := sqlite.Open(dataDir)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = reopened.Close() }()
	restoredEngine := reviewcore.New(reviewcore.Deps{Store: reopened, Sessions: reopened, PRs: reopened, Projects: reopened, Launcher: &localReviewLauncher{}, DataDir: dataDir})
	defer restoredEngine.CloseLocalReviews()
	restoredCoordinator := localworkflow.New(localworkflow.Deps{Store: reopened, Worker: model, Tests: localworkflow.NewTestRunner(dataDir), Reviews: reviewsvc.New(restoredEngine, reopened), DataDir: dataDir})
	defer func() { _ = restoredCoordinator.Close(ctx) }()
	if e = restoredCoordinator.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	restored, e := restoredCoordinator.Get(ctx, worker.ID)
	if e != nil {
		t.Fatal(e)
	}
	if restored.Current || restored.Stage != "local_review" || len(restored.Reviews) != 2 || len(restored.Tests) != 2 || restored.Reviews[0].ID != final.Reviews[0].ID || restored.Reviews[1].ID != final.Reviews[1].ID || restored.Reviews[0].LocalResult.Outcome != "changes_requested" || restored.Reviews[1].LocalResult.Outcome != "approved" {
		t.Fatalf("lost persisted round history after restart: %+v", restored)
	}
	if restored.Accepted || len(restored.Decisions) != 1 || restored.Decisions[0].Comment != "Manually checked" || restored.Decisions[0].ID != accepted.Decisions[0].ID {
		t.Fatalf("lost decision history or revived acceptance: %+v", restored)
	}

}
