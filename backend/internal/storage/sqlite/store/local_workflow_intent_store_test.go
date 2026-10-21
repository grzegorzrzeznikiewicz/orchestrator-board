// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type localWorkflowIntentQueue interface {
	ListPendingLocalWorkflowIntents(context.Context) ([]domain.LocalWorkflowIntent, error)
	ConsumeLocalWorkflowIntent(context.Context, domain.SessionID, string, string, time.Time) (bool, error)
}

func TestLocalWorkflowIntentChatHumanOnlyAndRetryDoesNotRearm(t *testing.T) {
	s, worker, conversation := conversationFixture(t)
	ctx := context.Background()
	if _, err := s.UpdateProjectSettings(ctx, "hist", "", domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal}); err != nil {
		t.Fatal(err)
	}
	queue := workflowIntentQueue(t, s)
	for i, msg := range []domain.ConversationMessage{
		{ID: "human", ClientMessageID: "human-client", Origin: domain.MessageOriginHuman, Text: "implement with tests"},
		{ID: "workflow-fix", ClientMessageID: "fix-client", Origin: domain.MessageOriginAutomation, Text: "repair review findings"},
		{ID: "relay-human", ClientMessageID: "relay-client", Origin: domain.MessageOriginAutomation, AuthoredByUser: true, Text: "also verify rounding"},
	} {
		turn := []string{"human-turn", "fix-turn", "relay-turn"}[i]
		created, err := s.AppendUserMessage(ctx, conversation, worker, "gen-1", msg, turn, histClock.Add(time.Duration(i)*time.Second))
		if err != nil || !created {
			t.Fatalf("append %s: %v %v", msg.ID, created, err)
		}
	}
	pending, err := queue.ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 2 {
		t.Fatalf("human intents: %+v %v", pending, err)
	}
	if pending[0].TurnID != "human-turn" || pending[0].Prompt != "implement with tests" || pending[1].TurnID != "relay-turn" || pending[1].Prompt != "also verify rounding" {
		t.Fatalf("wrong human scopes: %+v", pending)
	}
	if _, err := queue.ConsumeLocalWorkflowIntent(ctx, worker, pending[0].RequestID, "old-run", histClock.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	created, err := s.AppendUserMessage(ctx, conversation, worker, "gen-1", domain.ConversationMessage{ID: "retry-message", ClientMessageID: "human-client", Origin: domain.MessageOriginHuman, Text: "implement with tests"}, "retry-turn", histClock.Add(2*time.Minute))
	if err != nil || created {
		t.Fatalf("delivery retry: %v %v", created, err)
	}
	if pending, err := queue.ListPendingLocalWorkflowIntents(ctx); err != nil || len(pending) != 1 || pending[0].TurnID != "relay-turn" {
		t.Fatalf("retry rearmed: %+v %v", pending, err)
	}
}

func TestLocalWorkflowIntentHumanQueuesDuringVerificationWithoutAdmittingAutomation(t *testing.T) {
	s, worker, conversation := conversationFixture(t)
	ctx := context.Background()
	if _, err := s.UpdateProjectSettings(ctx, "hist", "", domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal}); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.CreateLocalWorkflow(ctx, domain.LocalWorkflowRun{ID: "verifying", SessionID: worker, Phase: domain.WorkflowTesting, Cycle: 1, MaxRounds: 3, CreatedAt: histClock, UpdatedAt: histClock}, "manual-start")
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.AppendUserMessage(ctx, conversation, worker, "gen-1", domain.ConversationMessage{ID: "human", Origin: domain.MessageOriginHuman, Text: "new acceptance criteria"}, "new-human-turn", histClock)
	if err != nil || !created {
		t.Fatalf("human intake refused during verification: %v %v", created, err)
	}
	pending, err := workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 1 || pending[0].Prompt != "new acceptance criteria" {
		t.Fatalf("missing durable human request: %+v %v", pending, err)
	}
	if _, err := s.AppendUserMessage(ctx, conversation, worker, "gen-1", domain.ConversationMessage{ID: "automation", Origin: domain.MessageOriginAutomation, Text: "unrelated automation"}, "automation-turn", histClock); err == nil {
		t.Fatal("verification admitted unrelated automation")
	}
	if err := s.CheckLocalWorkflowWorkerAdmission(ctx, worker); err == nil {
		t.Fatal("human intake released verification ownership")
	}
}

func TestLocalWorkflowIntentPromotionAndProjectionAreAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	localIntentProject(t, s, domain.ReviewTargetLocal)
	seed := intentSeed()
	seed.LocalWorkflowIntent = nil
	seed.IsTaskPreparation = true
	prepared, err := s.CreateSession(ctx, seed)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.PromoteTaskPreparation(ctx, prepared.ID, intentSeed()); err != nil || !ok {
		t.Fatalf("promotion: %v %v", ok, err)
	}
	current, _, err := s.GetSession(ctx, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Metadata.RuntimeLaunchID = "launch-1"
	if err := s.UpdateSession(ctx, current); err != nil {
		t.Fatal(err)
	}
	current, _, err = s.GetSession(ctx, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.LocalWorkflowIntent = &domain.LocalWorkflowIntent{RequestID: "tui:canonical-1", Prompt: "the actual submitted task", TurnID: "provider-turn-1", CreatedAt: histClock.Add(time.Second)}
	if ok, err := s.UpdateSessionFromActivitySignal(ctx, current, current.Revision-1); err != nil || ok {
		t.Fatalf("stale projection: %v %v", ok, err)
	}
	pending, err := workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 1 || pending[0].RequestID != "spawn:request-1" {
		t.Fatalf("stale CAS changed intent: %+v %v", pending, err)
	}
	if ok, err := s.UpdateSessionFromActivitySignal(ctx, current, current.Revision); err != nil || !ok {
		t.Fatalf("accepted projection: %v %v", ok, err)
	}
	pending, err = workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 1 || pending[0].RequestID != "tui:canonical-1" || pending[0].Prompt != "the actual submitted task" || pending[0].TurnID != "provider-turn-1" {
		t.Fatalf("canonical task scope: %+v %v", pending, err)
	}
}

func TestLocalWorkflowIntentSurvivesRestartWithoutDiscoveringOldWork(t *testing.T) {
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
	localIntentProject(t, s, domain.ReviewTargetLocal)
	old := intentSeed()
	old.LocalWorkflowIntent = nil
	if _, err := s.CreateSession(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateSession(context.Background(), intentSeed())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = nil
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(context.Background())
	if err != nil || len(pending) != 1 || pending[0].SessionID != created.ID {
		t.Fatalf("recovered wrong work: %+v %v", pending, err)
	}
}

func workflowIntentQueue(t *testing.T, s *sqlite.Store) localWorkflowIntentQueue {
	t.Helper()
	queue, ok := any(s).(localWorkflowIntentQueue)
	if !ok {
		t.Fatal("store does not persist and expose pending local workflow intents")
	}
	return queue
}

func localIntentProject(t *testing.T, s *sqlite.Store, mode domain.ReviewTargetKind) {
	t.Helper()
	if err := s.UpsertProject(context.Background(), domain.ProjectRecord{ID: "intent", Path: "/intent", RegisteredAt: histClock,
		Config: domain.ProjectConfig{ReviewMode: mode}}); err != nil {
		t.Fatal(err)
	}
}

func intentSeed() domain.SessionRecord {
	rec := sampleRecord("intent")
	rec.Harness = domain.HarnessCodex
	rec.Mode = domain.SessionModeTUI
	rec.LocalWorkflowIntent = &domain.LocalWorkflowIntent{RequestID: "spawn:request-1", Prompt: "implement the budget report with tests", CreatedAt: histClock}
	return rec
}

func TestLocalWorkflowIntentCommitsWithNewTaskAndConsumesOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	localIntentProject(t, s, domain.ReviewTargetLocal)
	worker, err := s.CreateSession(ctx, intentSeed())
	if err != nil {
		t.Fatal(err)
	}
	queue := workflowIntentQueue(t, s)
	pending, err := queue.ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	got := pending[0]
	if got.SessionID != worker.ID || got.RequestID != "spawn:request-1" || got.Prompt != "implement the budget report with tests" || got.State != "pending" || !got.CreatedAt.Equal(histClock) {
		t.Fatalf("wrong durable task scope: %+v", got)
	}
	if changed, err := queue.ConsumeLocalWorkflowIntent(ctx, worker.ID, got.RequestID, "workflow-1", histClock.Add(time.Minute)); err != nil || !changed {
		t.Fatalf("consume: %v %v", changed, err)
	}
	if changed, err := queue.ConsumeLocalWorkflowIntent(ctx, worker.ID, got.RequestID, "workflow-2", histClock.Add(2*time.Minute)); err != nil || changed {
		t.Fatalf("second consume: %v %v", changed, err)
	}
	if pending, err := queue.ListPendingLocalWorkflowIntents(ctx); err != nil || len(pending) != 0 {
		t.Fatalf("consumed intent reappeared: %+v %v", pending, err)
	}
}

func TestLocalWorkflowIntentDoesNotArmUnmarkedOrNonLocalSessions(t *testing.T) {
	for _, name := range []string{"old-unmarked", "pr", "orchestrator", "preparation", "blank"} {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			mode := domain.ReviewTargetLocal
			if name == "pr" {
				mode = domain.ReviewTargetPR
			}
			localIntentProject(t, s, mode)
			rec := intentSeed()
			switch name {
			case "old-unmarked":
				rec.LocalWorkflowIntent = nil
			case "orchestrator":
				rec.Kind = domain.KindOrchestrator
			case "preparation":
				rec.IsTaskPreparation = true
			case "blank":
				rec.LocalWorkflowIntent.Prompt = " \n "
			}
			if _, err := s.CreateSession(context.Background(), rec); err != nil {
				t.Fatal(err)
			}
			pending, err := workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(context.Background())
			if err != nil || len(pending) != 0 {
				t.Fatalf("unexpected automation: %+v %v", pending, err)
			}
		})
	}
}

func TestLocalWorkflowIntentRejectsUnsafeRequestAtomically(t *testing.T) {
	for _, request := range []string{"", "contains\nnewline", strings.Repeat("x", 257)} {
		t.Run(request, func(t *testing.T) {
			s := newTestStore(t)
			localIntentProject(t, s, domain.ReviewTargetLocal)
			rec := intentSeed()
			rec.LocalWorkflowIntent.RequestID = request
			if _, err := s.CreateSession(context.Background(), rec); err == nil {
				t.Fatal("unsafe request accepted")
			}
			workers, err := s.ListSessions(context.Background(), "intent")
			if err != nil || len(workers) != 0 {
				t.Fatalf("failed intent left a worker: %+v %v", workers, err)
			}
		})
	}
}
