package localworkflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// WorkerObservation reports correlated executor activity and its controller generation.
type WorkerObservation struct {
	State           string
	TurnID          string
	Generation      string
	ClientMessageID string
	Error           string
}

// Worker is the executor boundary used for observing and delivering required corrections.
type Worker interface {
	Observe(context.Context, domain.SessionID, string) (WorkerObservation, error)
	DeliverFix(context.Context, domain.SessionID, string, string) (WorkerObservation, error)
	Interrupt(context.Context, domain.SessionID, string) error
}

// WorkerStore reads durable session, message and turn evidence and can withdraw queued work.
type WorkerStore interface {
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	HasPendingLocalReviewWork(context.Context, domain.SessionID) (bool, error)
	LatestConversationTurn(context.Context, string) (domain.ConversationTurn, error)
	ConversationForSession(context.Context, domain.SessionID) (domain.ConversationRecord, error)
	ConversationMessageByClientID(context.Context, string, string) (domain.ConversationMessage, bool, error)
	TurnByID(context.Context, string) (domain.ConversationTurn, error)
}

// WorkerDelivery delivers an identified message through the existing session manager.
type WorkerDelivery interface {
	SendSemantic(context.Context, domain.SessionID, string, string) error
	InterruptTUI(context.Context, domain.SessionID) error
}

// ChatInterrupter requests cancellation through the existing Chat service.
type ChatInterrupter interface {
	Interrupt(context.Context, domain.SessionID) error
	CancelQueuedTurn(context.Context, domain.SessionID, string) error
}

// SessionWorker adapts durable Chat turns and session-manager delivery to workflow execution.
type SessionWorker struct {
	store    WorkerStore
	delivery WorkerDelivery
	chat     ChatInterrupter
}

// NewSessionWorker connects the existing worker services without creating an agent.
func NewSessionWorker(store WorkerStore, delivery WorkerDelivery, chat ChatInterrupter) *SessionWorker {
	return &SessionWorker{store, delivery, chat}
}

// WorkerGeneration includes both interface mode and the durable controller epoch.
func WorkerGeneration(r domain.SessionRecord) string { return domain.LocalWorkflowWorkerGeneration(r) }

// Observe correlates activity with the requested worker operation.
func (w *SessionWorker) Observe(ctx context.Context, id domain.SessionID, operationID string) (WorkerObservation, error) {
	r, ok, e := w.store.GetSession(ctx, id)
	if e != nil {
		return WorkerObservation{}, e
	}
	if !ok {
		return WorkerObservation{}, domain.ErrLocalWorkflowNotFound
	}
	obs := WorkerObservation{State: "uncertain", Generation: WorkerGeneration(r), ClientMessageID: operationID}
	if r.IsTerminated || r.Activity.State == domain.ActivityExited {
		obs.State = "interrupted"
		return obs, nil
	}
	if operationID == "" && (r.Activity.State == domain.ActivityBlocked || r.Activity.State == domain.ActivityWaitingInput) {
		obs.State = "needs_input"
		return obs, nil
	}
	if operationID == "" {
		pending, e := w.store.HasPendingLocalReviewWork(ctx, id)
		if e != nil {
			return obs, e
		}
		if pending || r.Activity.State == domain.ActivityActive {
			obs.State = "running"
		} else if r.Activity.State == domain.ActivityIdle {
			obs.State = "idle"
		}
		if obs.State == "idle" && domain.NormalizeSessionMode(r.Mode) == domain.SessionModeChat {
			conversation, err := w.store.ConversationForSession(ctx, id)
			if err != nil {
				return obs, err
			}
			turn, err := w.store.LatestConversationTurn(ctx, conversation.ID)
			if err != nil && !errors.Is(err, domain.ErrNoConversationTurn) {
				return obs, err
			}
			if err == nil && turn.ID != "" {
				if turn.HandledBySessionID != id || turn.HandledByReviewID != "" || turn.RolledBackAt != nil {
					return obs, fmt.Errorf("latest worker turn has changed owner")
				}
				obs.TurnID = turn.ID
				obs.Error = turn.ErrorMessage
				switch turn.State {
				case domain.TurnStateInterrupted, domain.TurnStateCancelled:
					obs.State = "interrupted"
				case domain.TurnStateFailed:
					obs.State = "failed"
				case domain.TurnStateRecovered:
					obs.State = "uncertain"
				case domain.TurnStateQueued:
					obs.State = "queued"
				case domain.TurnStateRunning:
					obs.State = "running"
				}
			}
		}
		return obs, nil
	}
	if domain.NormalizeSessionMode(r.Mode) != domain.SessionModeChat {
		// A semantic TUI acknowledgement proves acceptance, not successful completion.
		// Never turn a promptless idle/Stop or a cancelled turn into success.
		obs.Error = "Automatic corrections need Codex Chat's durable turn completion; switch this session to Chat and resume"
		return obs, nil
	}
	conversation, e := w.store.ConversationForSession(ctx, id)
	if e != nil {
		return obs, e
	}
	message, found, e := w.store.ConversationMessageByClientID(ctx, conversation.ID, operationID)
	if e != nil {
		return obs, e
	}
	if !found {
		obs.State = "absent"
		return obs, nil
	}
	turn, e := w.store.TurnByID(ctx, message.TurnID)
	if e != nil {
		return obs, e
	}
	if turn.HandledBySessionID != id || turn.HandledByReviewID != "" || turn.RolledBackAt != nil {
		return obs, fmt.Errorf("worker turn no longer belongs to this workflow's session")
	}
	obs.TurnID = turn.ID
	obs.Error = turn.ErrorMessage
	switch turn.State {
	case domain.TurnStateCompleted:
		obs.State = "completed"
	case domain.TurnStateRunning:
		obs.State = "running"
		if r.Activity.State.NeedsInput() {
			obs.State = "needs_input"
		}
	case domain.TurnStateQueued:
		obs.State = "queued"
	case domain.TurnStateFailed:
		obs.State = "failed"
	case domain.TurnStateInterrupted, domain.TurnStateCancelled:
		obs.State = "interrupted"
	}
	return obs, nil
}

// DeliverFix deduplicates a correction through its durable message identity.
func (w *SessionWorker) DeliverFix(ctx context.Context, id domain.SessionID, operationID, text string) (WorkerObservation, error) {
	if e := ctx.Err(); e != nil {
		return WorkerObservation{}, e
	}
	obs, e := w.Observe(ctx, id, operationID)
	if e != nil || obs.State != "absent" {
		return obs, e
	}
	if e := w.delivery.SendSemantic(ctx, id, text, operationID); e != nil {
		return obs, e
	}
	return w.Observe(ctx, id, operationID)
}

// Interrupt withdraws queued work or interrupts the owning active controller.
func (w *SessionWorker) Interrupt(ctx context.Context, id domain.SessionID, operationID string) error {
	obs, e := w.Observe(ctx, id, operationID)
	if e != nil {
		return e
	}
	if obs.State == "completed" || obs.State == "interrupted" || obs.State == "failed" || obs.State == "absent" {
		return nil
	}
	r, ok, e := w.store.GetSession(ctx, id)
	if e != nil {
		return e
	}
	if !ok {
		return domain.ErrLocalWorkflowNotFound
	}
	if domain.NormalizeSessionMode(r.Mode) == domain.SessionModeChat {
		if w.chat == nil {
			return fmt.Errorf("chat interruption unavailable")
		}
		if obs.State == "queued" {
			err := w.chat.CancelQueuedTurn(ctx, id, obs.TurnID)
			if err == nil {
				return nil
			}
			// Drain can win before cancellation acquires the controller lock. Inspect
			// its durable outcome and interrupt the same controller if now running.
			current, readErr := w.Observe(ctx, id, operationID)
			if readErr != nil {
				return readErr
			}
			switch current.State {
			case "completed", "interrupted", "failed", "absent":
				return nil
			case "running", "needs_input":
				return w.chat.Interrupt(ctx, id)
			default:
				return err
			}
		}
		return w.chat.Interrupt(ctx, id)
	}
	return w.delivery.InterruptTUI(ctx, id)
}
