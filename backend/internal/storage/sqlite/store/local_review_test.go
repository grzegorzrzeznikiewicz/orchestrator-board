package store_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestLocalReviewAndConfigurationSurviveReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlitetest.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	cfg := domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal, TestCommands: []string{"go test ./...", "npm test"}, Worker: domain.RoleOverride{Harness: domain.HarnessCodex}, Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerClaudeCode}}}
	if err = s.UpsertProject(ctx, domain.ProjectRecord{ID: "local", Path: "/local", RegisteredAt: time.Now(), Config: cfg}); err != nil {
		t.Fatal(err)
	}
	worker, err := s.CreateSession(ctx, sampleRecord("local"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err = s.UpsertReview(ctx, domain.Review{ID: "review-local", SessionID: worker.ID, ProjectID: worker.ProjectID, Harness: domain.ReviewerClaudeCode, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{ID: "local-run", ReviewID: "review-local", SessionID: worker.ID, Harness: domain.ReviewerClaudeCode, TargetKind: domain.ReviewTargetLocal, SnapshotID: "sha256-content", SnapshotPath: "/data/snapshot", SnapshotObservation: "stat-digest", SnapshotEpoch: "epoch-1", Status: domain.ReviewRunRunning, CreatedAt: now}
	if err = s.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err = s.InvalidateLocalReviewRun(ctx, run.ID, "file changed", now); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s = nil
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	project, ok, err := s.GetProject(ctx, "local")
	if err != nil || !ok || !reflect.DeepEqual(project.Config, cfg) {
		t.Fatalf("project=%+v ok=%v err=%v", project, ok, err)
	}
	got, ok, err := s.GetReviewRun(ctx, run.ID)
	if err != nil || !ok {
		t.Fatalf("read run: %v %v", ok, err)
	}
	if got.TargetKind != domain.ReviewTargetLocal || got.PRURL != "" || got.TargetSHA != "" || got.SnapshotID != run.SnapshotID || got.SnapshotPath != run.SnapshotPath || got.SnapshotObservation != run.SnapshotObservation || got.SnapshotEpoch != run.SnapshotEpoch || got.InvalidatedAt == nil || got.InvalidationReason != "file changed" {
		t.Fatalf("run=%+v", got)
	}
	runs, err := s.ListReviewRunsBySession(ctx, worker.ID)
	if err != nil || len(runs) != 1 || runs[0].SnapshotID != run.SnapshotID || runs[0].InvalidatedAt == nil {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
}

func TestWorkerDispatchInvalidatesLocalResultBeforeActivityHook(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	cfg := domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal}
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "local", Path: "/local", RegisteredAt: time.Now(), Config: cfg}); err != nil {
		t.Fatal(err)
	}
	worker, err := s.CreateSession(ctx, sampleRecord("local"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err = s.UpsertReview(ctx, domain.Review{ID: "r", SessionID: worker.ID, ProjectID: worker.ProjectID, Harness: domain.ReviewerClaudeCode, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{ID: "run", ReviewID: "r", SessionID: worker.ID, Harness: domain.ReviewerClaudeCode, TargetKind: domain.ReviewTargetLocal, SnapshotID: "snapshot", SnapshotPath: "/snapshot", Status: domain.ReviewRunRunning, CreatedAt: now}
	if err = s.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginLocalReviewEditing(ctx, worker.ID); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.GetReviewRun(ctx, run.ID)
	if err != nil || got.InvalidatedAt == nil {
		t.Fatalf("dispatch left review current: %+v %v", got, err)
	}
	if changed, err := s.UpdateReviewRunResult(ctx, run.ID, domain.ReviewRunComplete, domain.VerdictApproved, "ok", "", false); err != nil || changed {
		t.Fatalf("invalidated run accepted result: %v %v", changed, err)
	}
	current, _, err := s.GetSession(ctx, worker.ID)
	if err != nil || current.Activity.State != domain.ActivityActive {
		t.Fatalf("worker remained idle after dispatch: %+v %v", current.Activity, err)
	}
}

func TestQueuedNativeWorkerTurnInvalidatesLocalReview(t *testing.T) {
	s, worker, conversation := seededChatConversation(t)
	ctx := context.Background()
	now := time.Now()
	if err := s.UpsertReview(ctx, domain.Review{ID: "local-native", SessionID: worker.ID, ProjectID: worker.ProjectID, Harness: domain.ReviewerClaudeCode, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{ID: "native-run", ReviewID: "local-native", SessionID: worker.ID, Harness: domain.ReviewerClaudeCode, TargetKind: domain.ReviewTargetLocal, SnapshotID: "snapshot", SnapshotPath: "/snapshot", Status: domain.ReviewRunRunning, CreatedAt: now}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	created, err := s.AppendUserMessage(ctx, conversation.ID, worker.ID, "generation-root", domain.ConversationMessage{ID: "new-work", Text: "Continue editing", Origin: domain.MessageOriginHuman}, "new-turn", now)
	if err != nil || !created {
		t.Fatalf("message acceptance: %v %v", created, err)
	}
	got, _, err := s.GetReviewRun(ctx, run.ID)
	if err != nil || got.InvalidatedAt == nil {
		t.Fatalf("queued work left review current: %+v %v", got, err)
	}
	pending, err := s.HasPendingLocalReviewWork(ctx, worker.ID)
	if err != nil || !pending {
		t.Fatalf("queued work did not block snapshot: %v %v", pending, err)
	}
}
