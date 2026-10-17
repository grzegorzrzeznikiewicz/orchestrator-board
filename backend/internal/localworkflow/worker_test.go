package localworkflow

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type workerMemory struct {
	session domain.SessionRecord
	message domain.ConversationMessage
	turn    domain.ConversationTurn
	pending bool
}

func (s *workerMemory) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return s.session, true, nil
}
func (s *workerMemory) HasPendingLocalReviewWork(context.Context, domain.SessionID) (bool, error) {
	return s.pending, nil
}
func (s *workerMemory) ConversationForSession(context.Context, domain.SessionID) (domain.ConversationRecord, error) {
	return domain.ConversationRecord{ID: "conversation"}, nil
}
func (s *workerMemory) ConversationMessageByClientID(context.Context, string, string) (domain.ConversationMessage, bool, error) {
	return s.message, s.message.ID != "", nil
}
func (s *workerMemory) TurnByID(context.Context, string) (domain.ConversationTurn, error) {
	return s.turn, nil
}
func (s *workerMemory) CancelQueuedTurnByID(_ context.Context, _ string, _ string, _ time.Time) error {
	return nil
}

type workerDispatch struct {
	store *workerMemory
	sends int
}

func (d *workerDispatch) SendSemantic(_ context.Context, _ domain.SessionID, _ string, id string) error {
	d.sends++
	d.store.message = domain.ConversationMessage{ID: "message", TurnID: "turn", ClientMessageID: id}
	d.store.turn = domain.ConversationTurn{ID: "turn", HandledBySessionID: d.store.session.ID, State: domain.TurnStateRunning}
	return nil
}
func (d *workerDispatch) InterruptTUI(context.Context, domain.SessionID) error { return nil }
func (d *workerDispatch) Interrupt(context.Context, domain.SessionID) error {
	d.store.turn.State = domain.TurnStateInterrupted
	return nil
}

func TestWorkerCorrelatesFixAndDoesNotDeliverTwice(t *testing.T) {
	store := &workerMemory{session: domain.SessionRecord{ID: "worker", Mode: domain.SessionModeChat, Harness: domain.HarnessCodex, Activity: domain.Activity{State: domain.ActivityIdle}, Metadata: domain.SessionMetadata{ControllerGeneration: "gen"}}}
	dispatch := &workerDispatch{store: store}
	worker := NewSessionWorker(store, dispatch, dispatch)
	first, e := worker.DeliverFix(context.Background(), "worker", "fix-operation", "correct validation")
	if e != nil || first.State != "running" || first.TurnID != "turn" {
		t.Fatalf("delivery=%+v %v", first, e)
	}
	second, e := worker.DeliverFix(context.Background(), "worker", "fix-operation", "correct validation")
	if e != nil || dispatch.sends != 1 || second.TurnID != "turn" {
		t.Fatalf("duplicate=%+v sends=%d %v", second, dispatch.sends, e)
	}
	store.turn.State = domain.TurnStateCompleted
	done, e := worker.Observe(context.Background(), "worker", "fix-operation")
	if e != nil || done.State != "completed" || done.Generation != "chat:gen" {
		t.Fatalf("done=%+v %v", done, e)
	}
	store.turn.HandledBySessionID = "another-worker"
	wrong, e := worker.Observe(context.Background(), "worker", "fix-operation")
	if e == nil && wrong.State == "completed" {
		t.Fatal("accepted another worker's turn")
	}
}

func (s *workerMemory) LatestConversationTurn(context.Context, string) (domain.ConversationTurn, error) {
	return s.turn, nil
}
func TestWorkerInitialFailureIsNotSuccessfulIdle(t *testing.T) {
	store := &workerMemory{session: domain.SessionRecord{ID: "worker", Mode: domain.SessionModeChat, Activity: domain.Activity{State: domain.ActivityIdle}, Metadata: domain.SessionMetadata{ControllerGeneration: "gen"}}, turn: domain.ConversationTurn{ID: "failed-turn", HandledBySessionID: "worker", State: domain.TurnStateInterrupted}}
	worker := NewSessionWorker(store, &workerDispatch{store: store}, nil)
	observation, e := worker.Observe(context.Background(), "worker", "")
	if e != nil {
		t.Fatal(e)
	}
	if observation.State != "interrupted" || observation.TurnID != "failed-turn" {
		t.Fatalf("interrupted initial task became ready for tests: %+v", observation)
	}
}

func (d *workerDispatch) CancelQueuedTurn(_ context.Context, _ domain.SessionID, turnID string) error {
	if turnID != d.store.turn.ID {
		return fmt.Errorf("wrong turn")
	}
	d.store.turn.State = domain.TurnStateCancelled
	return nil
}
func TestQueuedWorkflowFixIsCancelledThroughController(t *testing.T) {
	for _, activity := range []domain.ActivityState{domain.ActivityIdle, domain.ActivityBlocked, domain.ActivityWaitingInput} {
		t.Run(string(activity), func(t *testing.T) {
			store := &workerMemory{session: domain.SessionRecord{ID: "worker", Mode: domain.SessionModeChat, Activity: domain.Activity{State: activity}}, message: domain.ConversationMessage{ID: "message", TurnID: "turn"}, turn: domain.ConversationTurn{ID: "turn", HandledBySessionID: "worker", State: domain.TurnStateQueued}}
			dispatch := &workerDispatch{store: store}
			worker := NewSessionWorker(store, dispatch, dispatch)
			if err := worker.Interrupt(context.Background(), "worker", "op"); err != nil {
				t.Fatal(err)
			}
			if store.turn.State != domain.TurnStateCancelled {
				t.Fatal("queued cancellation bypassed serialized Chat controller")
			}
		})
	}
}
