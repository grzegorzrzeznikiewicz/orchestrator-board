package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	reviewsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// Only the external agent process is replaced. HTTP, service, engine, snapshot
// capture and SQLite below are the production path.
type localReviewLauncher struct{ spec reviewcore.LaunchSpec }

func (*localReviewLauncher) InterfaceMode(domain.ReviewerHarness) domain.ReviewerInterfaceMode {
	return domain.ReviewerInterfaceTUI
}
func (*localReviewLauncher) Preflight(context.Context, domain.ReviewerHarness, string, ...domain.ReviewerInterfaceMode) error {
	return nil
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
	request, _ := json.Marshal(map[string]string{"runId": run.ID, "verdict": "approved", "body": "Reviewed local contents"})
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
			request, _ = json.Marshal(map[string]string{"runId": next.ID, "verdict": "approved", "body": "Review"})
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
