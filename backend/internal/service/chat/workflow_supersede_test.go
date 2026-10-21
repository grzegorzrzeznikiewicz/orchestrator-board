// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).
package chat_test

import (
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestWorkflowCancellationPreservesNewHumanInstruction(t *testing.T) {
	provider := newFakeConversation()
	provider.onSend = func(id string) {
		provider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: id})
	}
	h := newHarnessWithConversation(t, interruptingReviewConversation{provider})
	ctx := t.Context()
	fix, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "Old workflow correction", ClientMessageID: "old-workflow-fix"})
	if err != nil {
		t.Fatal(err)
	}
	human, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "New human instruction", Origin: domain.MessageOriginHuman, ClientMessageID: "new-human-instruction"})
	if err != nil || human.State != domain.TurnStateQueued {
		t.Fatalf("queue new human instruction: %+v %v", human, err)
	}
	worker := localworkflow.NewSessionWorker(h.st, nil, h.svc)
	if err := worker.Interrupt(ctx, testSession, "old-workflow-fix"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		old, err := h.st.TurnByID(ctx, fix.ID)
		if err != nil {
			t.Fatal(err)
		}
		current, err := h.st.TurnByID(ctx, human.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State.Terminal() {
			t.Fatal("stopping the old workflow cancelled the new human instruction")
		}
		if old.State.Terminal() && current.State == domain.TurnStateRunning {
			return
		}
		if old.State.Terminal() {
			// The coordinator drains preserved human input after the previous
			// workflow is quiescent; an interrupted turn does not auto-dispatch it.
			if err := h.svc.DrainQueued(ctx, testSession); err != nil {
				t.Fatal(err)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("old correction did not release new human instruction: old=%s human=%s", old.State, current.State)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestExplicitWorkflowStopCancelsQueuedHumanInstructions(t *testing.T) {
	provider := newFakeConversation()
	provider.onSend = func(id string) {
		provider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: id})
	}
	h := newHarnessWithConversation(t, interruptingReviewConversation{provider})
	ctx := t.Context()
	if _, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "Initial task"}); err != nil {
		t.Fatal(err)
	}
	queued, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "Queued instruction"})
	if err != nil {
		t.Fatal(err)
	}
	worker := localworkflow.NewSessionWorker(h.st, nil, h.svc)
	if err := worker.Interrupt(ctx, testSession, ""); err != nil {
		t.Fatal(err)
	}
	h.awaitSnapshot(t, func(snapshot store.ConversationSnapshot) bool {
		for _, turn := range snapshot.Turns {
			if turn.ID == queued.ID {
				return turn.State == domain.TurnStateInterrupted
			}
		}
		return false
	})
}

func TestWorkflowCancellationRefusalFinalizesOnlyOldEvidence(t *testing.T) {
	provider := newInterruptRecorder() // The provider has no active turn to cancel.
	provider.onSend = func(id string) {
		provider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: id})
	}
	h := newHarnessWithConversation(t, provider)
	ctx := t.Context()
	fix, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "Old correction", ClientMessageID: "old-fix"})
	if err != nil {
		t.Fatal(err)
	}
	provider.emit(
		ports.ChatEvent{Kind: ports.ChatEventMessageDelta, ProviderTurnID: "provider-turn-1", ProviderItemID: "old-output", Delta: "Partial result"},
		ports.ChatEvent{Kind: ports.ChatEventActivityStarted, ProviderTurnID: "provider-turn-1", ProviderItemID: "old-command", ActivityKind: domain.ActivityKindCommand, ActivityStatus: domain.ActivityStatusRunning, Summary: "Old command"},
	)
	h.awaitSnapshot(t, func(snapshot store.ConversationSnapshot) bool {
		return len(snapshot.Messages) == 2 && snapshot.Messages[1].Streaming && len(snapshot.Activities) == 1
	})
	human, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "New instruction", ClientMessageID: "human-after-fix"})
	if err != nil {
		t.Fatal(err)
	}
	worker := localworkflow.NewSessionWorker(h.st, nil, h.svc)
	if err := worker.Interrupt(ctx, testSession, "old-fix"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := h.st.LoadConversationSnapshot(ctx, h.ctrl.ConversationID())
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range snapshot.Messages {
		if message.TurnID == fix.ID && message.Streaming {
			t.Error("interrupted correction left its assistant output streaming")
		}
	}
	for _, activity := range snapshot.Activities {
		if activity.TurnID == fix.ID && activity.Status == domain.ActivityStatusRunning {
			t.Error("interrupted correction left its command running")
		}
	}
	current, err := h.st.TurnByID(ctx, human.ID)
	if err != nil || current.State.Terminal() {
		t.Fatalf("human instruction lost during reconciliation: %+v %v", current, err)
	}
}
