// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	reviewsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// Only the external agent process is replaced. HTTP, service, engine, snapshot
// capture and SQLite below are the production path.
type localReviewLauncher struct {
	spec         reviewcore.LaunchSpec
	preflightErr error
}

func TestLocalReviewRejectsUnstructuredApproval(t *testing.T) {
	ctx := context.Background()
	store, _, _, srv, worker := localProtocolFixture(t)
	path := "/api/v1/sessions/" + string(worker.ID) + "/reviews"
	body, status, _ := doRequest(t, srv, http.MethodPost, path+"/trigger", `{}`)
	if status != http.StatusCreated {
		t.Fatalf("trigger: %d %s", status, body)
	}
	var trigger controllers.TriggerReviewResponse
	if err := json.Unmarshal(body, &trigger); err != nil {
		t.Fatal(err)
	}
	run := trigger.Runs[0]
	request, _ := json.Marshal(map[string]string{"runId": run.ID, "verdict": "approved", "body": "Looks good"})
	body, status, _ = doRequest(t, srv, http.MethodPost, path+"/submit", string(request))
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unstructured approval accepted: %d %s", status, body)
	}
	got, _, err := store.GetReviewRun(ctx, run.ID)
	if err != nil || got.Status != domain.ReviewRunFailed || got.Verdict != domain.VerdictNone {
		t.Fatalf("malformed review did not fail closed: %+v %v", got, err)
	}
}

func (*localReviewLauncher) InterfaceMode(domain.ReviewerHarness) domain.ReviewerInterfaceMode {
	return domain.ReviewerInterfaceTUI
}
func (l *localReviewLauncher) Preflight(context.Context, domain.ReviewerHarness, string, ...domain.ReviewerInterfaceMode) error {
	return l.preflightErr
}
func (l *localReviewLauncher) Spawn(_ context.Context, s reviewcore.LaunchSpec) (reviewcore.LaunchResult, error) {
	l.spec = s
	return reviewcore.LaunchResult{HandleID: "local-review", LaunchID: s.LaunchID}, nil
}
func (*localReviewLauncher) RestoreTerminal(context.Context, reviewcore.LaunchSpec) (reviewcore.LaunchResult, error) {
	return reviewcore.LaunchResult{}, nil
}
func (*localReviewLauncher) Notify(context.Context, string, reviewcore.LaunchSpec) error  { return nil }
func (*localReviewLauncher) Alive(context.Context, string, string) (bool, error)          { return true, nil }
func (*localReviewLauncher) Reusable(domain.ReviewerHarness) bool                         { return false }
func (*localReviewLauncher) Cancel(context.Context, string, domain.ReviewerHarness) error { return nil }
func (*localReviewLauncher) Destroy(context.Context, string) error                        { return nil }

type localSubmissionStore struct {
	*sqlite.Store
	beforeComplete func()
	afterComplete  func()
}

func (s *localSubmissionStore) UpdateReviewRunResult(ctx context.Context, id string, status domain.ReviewRunStatus, verdict domain.ReviewVerdict, body, githubID string, inject bool) (bool, error) {
	if status == domain.ReviewRunComplete && s.beforeComplete != nil {
		s.beforeComplete()
	}
	updated, err := s.Store.UpdateReviewRunResult(ctx, id, status, verdict, body, githubID, inject)
	if status == domain.ReviewRunComplete && s.afterComplete != nil {
		s.afterComplete()
	}
	return updated, err
}

func TestLocalReviewHTTPWithoutPRAndStaleResult(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	c := exec.Command("git", "init", "root")
	c.Dir = root
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("init: %s %v", out, err)
	}
	root = filepath.Join(root, "root")
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &localSubmissionStore{Store: sqlitetest.MustOpen(t)}
	now := time.Now().UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "local", Path: root, RegisteredAt: now, Config: domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal}}); err != nil {
		t.Fatal(err)
	}
	worker, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: "local", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}, Metadata: domain.SessionMetadata{WorkspacePath: root}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	launcher := &localReviewLauncher{}
	engine := reviewcore.New(reviewcore.Deps{Store: store, Sessions: store, Projects: store, Launcher: launcher, DataDir: t.TempDir()})
	t.Cleanup(engine.CloseLocalReviews)
	srv := newReviewTestServer(t, reviewsvc.New(engine, store))
	path := "/api/v1/sessions/" + string(worker.ID) + "/reviews"
	body, status, _ := doRequest(t, srv, http.MethodPost, path+"/trigger", `{}`)
	if status != http.StatusCreated {
		t.Fatalf("trigger status=%d body=%s", status, body)
	}
	var trigger controllers.TriggerReviewResponse
	if err = json.Unmarshal(body, &trigger); err != nil {
		t.Fatal(err)
	}
	if len(trigger.Runs) != 1 || trigger.Runs[0].SnapshotID == "" || launcher.spec.Harness != domain.ReviewerClaudeCode || launcher.spec.PRURL != "" {
		t.Fatalf("trigger=%+v spec=%+v", trigger, launcher.spec)
	}
	run := trigger.Runs[0]
	request := localResultRequest(run, "Reviewed local contents")
	body, status, _ = doRequest(t, srv, http.MethodPost, path+"/submit", string(request))
	if status != http.StatusOK {
		t.Fatalf("submit status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, http.MethodPost, path+"/submit", string(request))
	var repeated controllers.ReviewRunResponse
	if err = json.Unmarshal(body, &repeated); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || !repeated.Review.Current {
		t.Fatalf("idempotent local result lost freshness: %d %s", status, body)
	}
	body, status, _ = doRequest(t, srv, http.MethodGet, path, "")
	var list controllers.ListReviewsResponse
	if err = json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || len(list.Reviews) != 1 || list.Reviews[0].Status != reviewcore.ReviewStateUpToDate || !list.Runs[0].Current {
		t.Fatalf("fresh result status=%d list=%+v body=%s", status, list, body)
	}
	if err = os.WriteFile(filepath.Join(root, "new.txt"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	body, status, _ = doRequest(t, srv, http.MethodGet, path, "")
	list = controllers.ListReviewsResponse{}
	if err = json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || list.Reviews[0].Status != reviewcore.ReviewStateNeedsReview || list.Runs[0].Current || list.Runs[0].InvalidatedAt == nil {
		t.Fatalf("stale result status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, http.MethodPost, path+"/submit", string(request))
	if status != http.StatusConflict {
		t.Fatalf("stale result accepted: status=%d body=%s", status, body)
	}
	for _, stage := range []string{"before", "after"} {
		t.Run("mutation_"+stage+"_result_write", func(t *testing.T) {
			body, status, _ = doRequest(t, srv, http.MethodPost, path+"/trigger", `{"rerun":true}`)
			if status != http.StatusCreated {
				t.Fatalf("rerun status=%d body=%s", status, body)
			}
			trigger = controllers.TriggerReviewResponse{}
			if err = json.Unmarshal(body, &trigger); err != nil {
				t.Fatal(err)
			}
			next := trigger.Runs[0]
			request = localResultRequest(next, "Review")
			if stage == "before" {
				store.beforeComplete = func() {
					if err := store.InvalidateLocalReviewRun(ctx, next.ID, "concurrent mutation", time.Now()); err != nil {
						t.Error(err)
					}
				}
			} else {
				store.afterComplete = func() {
					if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("three"), 0600); err != nil {
						t.Error(err)
					}
				}
			}
			body, status, _ = doRequest(t, srv, http.MethodPost, path+"/submit", string(request))
			store.beforeComplete = nil
			store.afterComplete = nil
			if status != http.StatusConflict {
				t.Fatalf("racing result accepted: %d %s", status, body)
			}
		})
	}

}

func localResultRequest(run domain.ReviewRun, summary string) []byte {
	raw, _ := json.Marshal(map[string]any{"runId": run.ID, "localResult": domain.LocalReviewResult{Version: 1, SnapshotID: run.SnapshotID, Round: run.Round, Outcome: "approved", Summary: summary, Findings: []domain.LocalReviewFinding{}}})
	return raw
}

func (s *localSubmissionStore) UpdateLocalReviewResult(ctx context.Context, id string, status domain.ReviewRunStatus, verdict domain.ReviewVerdict, body string, result *domain.LocalReviewResult) (bool, error) {
	if status == domain.ReviewRunComplete && s.beforeComplete != nil {
		s.beforeComplete()
	}
	updated, err := s.Store.UpdateLocalReviewResult(ctx, id, status, verdict, body, result)
	if status == domain.ReviewRunComplete && s.afterComplete != nil {
		s.afterComplete()
	}
	return updated, err
}

func localProtocolFixture(t *testing.T, options ...reviewsvc.Option) (*sqlite.Store, *reviewcore.Engine, *localReviewLauncher, *httptest.Server, domain.SessionRecord) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	if out, err := exec.Command("git", "init", root).CombinedOutput(); err != nil {
		t.Fatalf("init: %s %v", out, err)
	}
	store := sqlitetest.MustOpen(t)
	now := time.Now().UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "local", Path: root, RegisteredAt: now, Config: domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal}}); err != nil {
		t.Fatal(err)
	}
	worker, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: "local", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}, Metadata: domain.SessionMetadata{WorkspacePath: root, Prompt: "Fix login. Acceptance criteria: invalid credentials remain rejected."}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	launcher := &localReviewLauncher{}
	engine := reviewcore.New(reviewcore.Deps{Store: store, Sessions: store, Projects: store, Launcher: launcher, DataDir: t.TempDir()})
	t.Cleanup(engine.CloseLocalReviews)
	srv := newReviewTestServer(t, reviewsvc.New(engine, store, options...))
	return store, engine, launcher, srv, worker
}

func TestLocalReviewProtocolHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		mutate        func(*domain.LocalReviewResult)
		wantStatus    int
		wantState     domain.ReviewRunStatus
		wantVerdict   domain.ReviewVerdict
	}{
		{"approval", "approved", nil, 200, domain.ReviewRunComplete, domain.VerdictApproved},
		{"suggestion is optional", "approved", func(r *domain.LocalReviewResult) {
			r.Findings = []domain.LocalReviewFinding{{Kind: "suggestion", Path: "new.txt", Line: 1, Reason: "Consider a clearer name"}}
		}, 200, domain.ReviewRunComplete, domain.VerdictApproved},
		{"required changes", "changes_requested", func(r *domain.LocalReviewResult) {
			r.Findings = []domain.LocalReviewFinding{{Kind: "required", Path: "new.txt", Line: 1, Reason: "Invalid credentials are accepted"}}
		}, 200, domain.ReviewRunComplete, domain.VerdictChangesRequested},
		{"reviewer error", "error", nil, 200, domain.ReviewRunFailed, domain.VerdictNone},
		{"wrong snapshot", "approved", func(r *domain.LocalReviewResult) { r.SnapshotID = "other" }, 422, domain.ReviewRunFailed, domain.VerdictNone},
		{"wrong round", "approved", func(r *domain.LocalReviewResult) { r.Round++ }, 422, domain.ReviewRunFailed, domain.VerdictNone},
		{"missing findings", "approved", func(r *domain.LocalReviewResult) { r.Findings = nil }, 422, domain.ReviewRunFailed, domain.VerdictNone},
		{"unsupported version", "approved", func(r *domain.LocalReviewResult) { r.Version = 99 }, 422, domain.ReviewRunFailed, domain.VerdictNone},
		{"contradictory approval", "approved", func(r *domain.LocalReviewResult) {
			r.Findings = []domain.LocalReviewFinding{{Kind: "required", Path: "new.txt", Line: 1, Reason: "Bug"}}
		}, 422, domain.ReviewRunFailed, domain.VerdictNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifications := &localNotificationRecorder{}
			store, _, launcher, srv, worker := localProtocolFixture(t, reviewsvc.WithNotificationSink(notifications))
			if err := os.WriteFile(filepath.Join(worker.Metadata.WorkspacePath, "new.txt"), []byte("new source file"), 0600); err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/sessions/" + string(worker.ID) + "/reviews"
			raw, status, _ := doRequest(t, srv, http.MethodPost, path+"/trigger", `{}`)
			if status != 201 {
				t.Fatalf("trigger: %d %s", status, raw)
			}
			var trigger controllers.TriggerReviewResponse
			if err := json.Unmarshal(raw, &trigger); err != nil {
				t.Fatal(err)
			}
			run := trigger.Runs[0]
			if run.Round != 1 {
				t.Fatalf("first round=%d", run.Round)
			}
			if captured, err := os.ReadFile(filepath.Join(launcher.spec.WorkspacePath, "files", "new.txt")); err != nil || string(captured) != "new source file" {
				t.Fatalf("new file not captured: %s %v", captured, err)
			}
			result := domain.LocalReviewResult{Version: 1, SnapshotID: run.SnapshotID, Round: run.Round, Outcome: tc.outcome, Summary: "Acceptance criteria assessed", Findings: []domain.LocalReviewFinding{}}
			if tc.mutate != nil {
				tc.mutate(&result)
			}
			request, _ := json.Marshal(map[string]any{"reviews": []any{map[string]any{"runId": run.ID, "localResult": result}}})
			raw, status, _ = doRequest(t, srv, http.MethodPost, path+"/submit", string(request))
			if status != tc.wantStatus {
				t.Fatalf("submit: %d %s", status, raw)
			}
			got, _, err := store.GetReviewRun(context.Background(), run.ID)
			if err != nil || got.Status != tc.wantState || got.Verdict != tc.wantVerdict {
				t.Fatalf("persisted=%+v err=%v", got, err)
			}
			if tc.outcome == "error" || tc.wantStatus != 200 {
				if len(notifications.intents) != 0 {
					t.Fatalf("failure emitted approval notification: %+v", notifications.intents)
				}
			}
			if tc.wantStatus == 200 {
				if got.LocalResult == nil || got.LocalResult.Outcome != tc.outcome {
					t.Fatalf("structured result missing: %+v", got)
				}
				raw, status, _ = doRequest(t, srv, http.MethodPost, path+"/submit", string(request))
				if status != 200 {
					t.Fatalf("identical retry failed: %d %s", status, raw)
				}
			}
		})
	}
}

func TestLocalReviewUnavailableClaudeIsDurableFailure(t *testing.T) {
	store, engine, launcher, _, worker := localProtocolFixture(t)
	launcher.preflightErr = errors.New("Claude unavailable")
	if _, err := engine.TriggerWithOptions(context.Background(), worker.ID, reviewcore.TriggerOptions{}); err == nil {
		t.Fatal("unavailable reviewer reported success")
	}
	runs, err := store.ListReviewRunsBySession(context.Background(), worker.ID)
	if err != nil || len(runs) != 1 || runs[0].Status != domain.ReviewRunFailed || runs[0].Verdict != domain.VerdictNone || launcher.spec.RunID != "" {
		t.Fatalf("unavailable reviewer state=%+v err=%v", runs, err)
	}
}

func TestLocalReviewHTTPRejectsTrailingData(t *testing.T) {
	for _, suffix := range []string{"garbage", `{ "verdict": "changes_requested" }`} {
		t.Run(suffix, func(t *testing.T) {
			store, _, _, srv, worker := localProtocolFixture(t)
			path := "/api/v1/sessions/" + string(worker.ID) + "/reviews"
			raw, status, _ := doRequest(t, srv, http.MethodPost, path+"/trigger", `{}`)
			if status != 201 {
				t.Fatalf("trigger: %d %s", status, raw)
			}
			var trigger controllers.TriggerReviewResponse
			if err := json.Unmarshal(raw, &trigger); err != nil {
				t.Fatal(err)
			}
			run := trigger.Runs[0]
			raw, status, _ = doRequest(t, srv, http.MethodPost, path+"/submit", string(localResultRequest(run, "Reviewed"))+suffix)
			if status != 400 {
				t.Fatalf("malformed request accepted: %d %s", status, raw)
			}
			got, _, err := store.GetReviewRun(context.Background(), run.ID)
			if err != nil || got.Verdict != domain.VerdictNone {
				t.Fatalf("malformed result approved: %+v %v", got, err)
			}
		})
	}
}

func TestLocalReviewDoesNotPartiallyApproveAmbiguousBatch(t *testing.T) {
	store, _, _, srv, worker := localProtocolFixture(t)
	path := "/api/v1/sessions/" + string(worker.ID) + "/reviews"
	raw, status, _ := doRequest(t, srv, http.MethodPost, path+"/trigger", `{}`)
	if status != 201 {
		t.Fatalf("trigger: %d %s", status, raw)
	}
	var trigger controllers.TriggerReviewResponse
	if err := json.Unmarshal(raw, &trigger); err != nil {
		t.Fatal(err)
	}
	run := trigger.Runs[0]
	request := `{"reviews":[` + string(localResultRequest(run, "Reviewed")) + `,` + string(localResultRequest(run, "Different result")) + `]}`
	raw, status, _ = doRequest(t, srv, http.MethodPost, path+"/submit", request)
	if status != 422 {
		t.Fatalf("ambiguous batch not rejected: %d %s", status, raw)
	}
	got, _, err := store.GetReviewRun(context.Background(), run.ID)
	if err != nil || got.Verdict != domain.VerdictNone {
		t.Fatalf("ambiguous batch partially approved: %+v %v", got, err)
	}
}

type localNotificationRecorder struct{ intents []ports.NotificationIntent }

func (r *localNotificationRecorder) Notify(_ context.Context, intent ports.NotificationIntent) error {
	r.intents = append(r.intents, intent)
	return nil
}
