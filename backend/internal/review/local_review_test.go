package review

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type preflightActionLauncher struct {
	*fakeLauncher
	action func()
}

func (l preflightActionLauncher) Preflight(context.Context, domain.ReviewerHarness, string, ...domain.ReviewerInterfaceMode) error {
	l.action()
	return nil
}

func TestLocalReviewDoesNotLaunchIfWorkerResumesDuringPreflight(t *testing.T) {
	e, _, launcher, worker := localEngine(t)
	e.launcher = preflightActionLauncher{fakeLauncher: launcher, action: func() {
		worker.Activity.State = domain.ActivityActive
		e.sessions = fakeSessions{rec: worker, ok: true}
	}}
	_, err := e.TriggerWithOptions(context.Background(), worker.ID, TriggerOptions{})
	if !errors.Is(err, ErrConflict) || launcher.spawned {
		t.Fatalf("resumed executor did not block reviewer: err=%v spawned=%v", err, launcher.spawned)
	}
}

func localEngine(t *testing.T) (*Engine, *sqlite.Store, *fakeLauncher, domain.SessionRecord) {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	s := newSQLiteReviewStore(t)
	worker := liveWorker()
	worker.Metadata.WorkspacePath = root
	worker.Harness = domain.HarnessCodex
	seedReviewWorker(t, s, worker)
	launcher := &fakeLauncher{handle: "local-reviewer", alive: true}
	e := New(Deps{Store: s, Sessions: s, Projects: fakeProjects{cfg: domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal}}, Launcher: launcher, DataDir: t.TempDir()})
	t.Cleanup(e.CloseLocalReviews)
	return e, s, launcher, worker
}

func TestLocalReviewLaunchesWithoutPR(t *testing.T) {
	e, s, launcher, worker := localEngine(t)
	res, err := e.TriggerWithOptions(context.Background(), worker.ID, TriggerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || !launcher.spawned || res.Run.TargetKind != domain.ReviewTargetLocal || res.Run.SnapshotID == "" || res.Run.TargetSHA != "" || res.Run.PRURL != "" || launcher.gotSpec.WorkspacePath == worker.Metadata.WorkspacePath {
		t.Fatalf("result=%+v spec=%+v", res, launcher.gotSpec)
	}
	if launcher.gotSpec.Harness != domain.ReviewerClaudeCode {
		t.Fatalf("reviewer=%s", launcher.gotSpec.Harness)
	}
	if _, err := os.Stat(filepath.Join(launcher.gotSpec.WorkspacePath, "files", "file.txt")); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetReviewRun(context.Background(), res.Run.ID)
	if err != nil || !ok || got.SnapshotID != res.Run.SnapshotID {
		t.Fatalf("persisted=%+v err=%v", got, err)
	}
	state, err := e.List(context.Background(), worker.ID)
	if err != nil || len(state.Reviews) != 1 || state.Reviews[0].TargetKind != domain.ReviewTargetLocal {
		t.Fatalf("list=%+v err=%v", state, err)
	}
}

func TestLocalReviewRejectsActiveOrUnknownWorker(t *testing.T) {
	for _, state := range []domain.ActivityState{domain.ActivityActive, "", domain.ActivityBlocked, domain.ActivityWaitingInput} {
		t.Run(string(state), func(t *testing.T) {
			e, _, launcher, worker := localEngine(t)
			worker.Activity.State = state
			e.sessions = fakeSessions{rec: worker, ok: true}
			_, err := e.TriggerWithOptions(context.Background(), worker.ID, TriggerOptions{})
			if !errors.Is(err, ErrConflict) || launcher.spawned {
				t.Fatalf("state=%s err=%v spawned=%v", state, err, launcher.spawned)
			}
		})
	}
}

func TestLocalReviewEditAndRevertStaysInvalid(t *testing.T) {
	e, s, _, worker := localEngine(t)
	ctx := context.Background()
	res, err := e.TriggerWithOptions(ctx, worker.ID, TriggerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(worker.Metadata.WorkspacePath, "file.txt")
	if err = os.WriteFile(path, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.ValidateLocalRun(ctx, res.Run); !errors.Is(err, ErrConflict) {
		t.Fatalf("reverted edit accepted: %v", err)
	}
	got, _, err := s.GetReviewRun(ctx, res.Run.ID)
	if err != nil || got.InvalidatedAt == nil {
		t.Fatalf("invalidation was not durable: %+v %v", got, err)
	}
}

func TestLocalReviewRestartRetainsIdentityButRejectsInFlightResult(t *testing.T) {
	e, s, launcher, worker := localEngine(t)
	ctx := context.Background()
	res, err := e.TriggerWithOptions(ctx, worker.ID, TriggerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e.CloseLocalReviews()
	restarted := New(Deps{Store: s, Sessions: s, Projects: e.projects, Launcher: launcher, DataDir: t.TempDir()})
	t.Cleanup(restarted.CloseLocalReviews)
	if err = restarted.ValidateLocalRun(ctx, res.Run); !errors.Is(err, ErrConflict) {
		t.Fatalf("unobserved in-flight review survived restart: %v", err)
	}
	got, _, err := s.GetReviewRun(ctx, res.Run.ID)
	if err != nil || got.SnapshotID != res.Run.SnapshotID || got.InvalidatedAt == nil {
		t.Fatalf("restart erased identity or revived result: %+v %v", got, err)
	}
}

func TestLocalReviewPromptDoesNotRequireProvider(t *testing.T) {
	prompt, system := reviewTexts(LaunchSpec{TargetKind: domain.ReviewTargetLocal, SnapshotID: "content-id", RunID: "local-run", WorkerID: "worker", WorkspacePath: "/snapshot"})
	if strings.Contains(prompt, "gh api") || strings.Contains(system, "Post your review as a comment on the pull request") || !strings.Contains(prompt, "content-id") || !strings.Contains(prompt, "ao review submit") || !strings.Contains(prompt, "manifest.json") || !strings.Contains(prompt, "--reviews -") || !strings.Contains(prompt, `"runId": "local-run"`) {
		t.Fatalf("invalid local instructions:\n%s\n%s", prompt, system)
	}
}

func TestCompletedLocalReviewCannotReviveAfterRestart(t *testing.T) {
	e, s, _, worker := localEngine(t)
	ctx := context.Background()
	res, err := e.TriggerWithOptions(ctx, worker.ID, TriggerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateReviewRunResult(ctx, res.Run.ID, domain.ReviewRunComplete, domain.VerdictApproved, "ok", "", false); err != nil {
		t.Fatal(err)
	}
	e.CloseLocalReviews()
	restarted := New(Deps{Store: s, Sessions: s, Projects: e.projects, Launcher: e.launcher, DataDir: e.localDataDir})
	defer restarted.CloseLocalReviews()
	if err = restarted.ValidateLocalRun(ctx, res.Run); !errors.Is(err, ErrConflict) {
		t.Fatalf("completed review revived after lost observation: %v", err)
	}
}

func TestLocalReviewSerializesSnapshotWithNewWorkerDelivery(t *testing.T) {
	e, s, launcher, worker := localEngine(t)
	ctx := context.Background()
	attempted := make(chan struct{})
	delivered := make(chan error, 1)
	e.launcher = preflightActionLauncher{fakeLauncher: launcher, action: func() {
		go func() {
			close(attempted)
			release, err := s.AcquireLocalReviewWorkspace(ctx, worker.ID)
			if err == nil {
				defer release()
				err = s.BeginLocalReviewEditing(ctx, worker.ID)
			}
			delivered <- err
		}()
		<-attempted
		select {
		case err := <-delivered:
			t.Fatalf("worker delivery raced snapshot preparation: %v", err)
		default:
		}
	}}
	result, err := e.TriggerWithOptions(ctx, worker.ID, TriggerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err = <-delivered; err != nil {
		t.Fatal(err)
	}
	// Even a quick active/idle cycle with no file edits cannot revive this run.
	worker, _, err = s.GetSession(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Activity.State = domain.ActivityIdle
	if err = s.UpdateSession(ctx, worker); err != nil {
		t.Fatal(err)
	}
	if err = e.ValidateLocalRun(ctx, result.Run); !errors.Is(err, ErrConflict) {
		t.Fatalf("new worker delivery left a current review: %v", err)
	}
}

func TestLocalReviewerRestoreRequiresFreshRun(t *testing.T) {
	e, _, launcher, worker := localEngine(t)
	ctx := context.Background()
	if _, err := e.TriggerWithOptions(ctx, worker.ID, TriggerOptions{}); err != nil {
		t.Fatal(err)
	}
	launcher.spawned = false
	result, err := e.RestoreReviewer(ctx, worker.ID)
	if err != nil || result.Restored || launcher.spawned {
		t.Fatalf("local reviewer restored through PR path: %+v %v", result, err)
	}
}

func TestLocalReviewTimeoutFailsWithoutAnotherRead(t *testing.T) {
	e, s, _, worker := localEngine(t)
	now := time.Now()
	e.clock = func() time.Time { return now }
	e.localTimeout = 80 * time.Millisecond
	res, err := e.TriggerWithOptions(context.Background(), worker.ID, TriggerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		run, _, err := s.GetReviewRun(context.Background(), res.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == domain.ReviewRunFailed {
			if run.Verdict != domain.VerdictNone || !strings.Contains(run.Body, "timed out") {
				t.Fatalf("timeout result=%+v", run)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("review remained running after timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type retryTimeoutStore struct {
	*sqlite.Store
	fail atomic.Bool
}

func (s *retryTimeoutStore) UpdateReviewRunResult(ctx context.Context, id string, status domain.ReviewRunStatus, verdict domain.ReviewVerdict, body, githubID string, inject bool) (bool, error) {
	if status == domain.ReviewRunFailed && s.fail.CompareAndSwap(true, false) {
		return false, errors.New("temporary write failure")
	}
	return s.Store.UpdateReviewRunResult(ctx, id, status, verdict, body, githubID, inject)
}

func TestLocalReviewRetriesFailedTimeoutPersistence(t *testing.T) {
	e, s, _, worker := localEngine(t)
	retry := &retryTimeoutStore{Store: s}
	retry.fail.Store(true)
	e.store = retry
	now := time.Now()
	e.clock = func() time.Time { return now }
	e.localTimeout = 80 * time.Millisecond
	res, err := e.TriggerWithOptions(context.Background(), worker.ID, TriggerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(4 * time.Second)
	for {
		run, _, err := s.GetReviewRun(context.Background(), res.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == domain.ReviewRunFailed {
			return
		}
		select {
		case <-deadline:
			t.Fatal("transient persistence error disabled timeout")
		case <-time.After(15 * time.Millisecond):
		}
	}
}

func TestLocalReviewCarriesTaskAndMonotonicRound(t *testing.T) {
	e, s, launcher, worker := localEngine(t)
	now := time.Now()
	e.clock = func() time.Time { return now }
	worker.Metadata.Prompt = "Fix login. Acceptance criteria: reject invalid credentials."
	worker.Metadata.LatestUserPrompt = "Also cover disabled users."
	if err := s.UpdateSession(context.Background(), worker); err != nil {
		t.Fatal(err)
	}
	first, err := e.TriggerWithOptions(context.Background(), worker.ID, TriggerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	prompt, _ := reviewTexts(launcher.gotSpec)
	for _, want := range []string{worker.Metadata.Prompt, worker.Metadata.LatestUserPrompt, `"round": 1`, `"findings"`, `"snapshotId"`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing %q in %s", want, prompt)
		}
	}
	if _, err = s.UpdateReviewRunResult(context.Background(), first.Run.ID, domain.ReviewRunFailed, domain.VerdictNone, "no response", "", false); err != nil {
		t.Fatal(err)
	}
	second, err := e.TriggerWithOptions(context.Background(), worker.ID, TriggerOptions{Rerun: true})
	if err != nil || first.Run.Round != 1 || second.Run.Round != 2 {
		t.Fatalf("rounds: first=%d second=%d err=%v", first.Run.Round, second.Run.Round, err)
	}
	if len(second.Reviews) != 1 || second.Reviews[0].Status != ReviewStateRunning || second.Reviews[0].LatestRun == nil || second.Reviews[0].LatestRun.Round != 2 {
		t.Fatalf("same-time rounds selected an older result: %+v", second.Reviews)
	}
}
