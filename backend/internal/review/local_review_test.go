package review

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
