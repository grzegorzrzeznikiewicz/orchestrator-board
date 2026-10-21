// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).
package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// CancelWorkflowTurn withdraws one workflow-owned correction. Unlike user Stop,
// replacing a workflow must preserve the new human instructions queued behind it.
func (s *Service) CancelWorkflowTurn(ctx context.Context, id domain.SessionID, turnID string) error {
	if _, err := s.requireChatSession(ctx, id); err != nil {
		return err
	}
	controller, err := s.Controller(id)
	if err != nil {
		return err
	}
	return controller.cancelWorkflowTurn(ctx, turnID)
}

func (c *Controller) cancelWorkflowTurn(ctx context.Context, turnID string) error {
	c.sendMu.Lock()
	if c.handoffActive() {
		c.sendMu.Unlock()
		return ErrControllerHandoff
	}
	turn, err := c.store.TurnByID(ctx, turnID)
	if err != nil {
		c.sendMu.Unlock()
		return err
	}
	if turn.ConversationID != c.conversation.ID || turn.HandledBySessionID != c.sessionID || turn.HandledByReviewID != "" || turn.RolledBackAt != nil {
		c.sendMu.Unlock()
		return fmt.Errorf("%w: %s", domain.ErrNoConversationTurn, turnID)
	}
	if turn.State.Terminal() {
		c.sendMu.Unlock()
		return nil
	}
	if turn.State == domain.TurnStateQueued {
		defer c.sendMu.Unlock()
		return c.store.CancelQueuedTurnByID(ctx, c.conversation.ID, turnID, c.now())
	}
	if turn.State != domain.TurnStateRunning || turn.ProviderTurnID == "" {
		c.sendMu.Unlock()
		return ErrNoActiveTurn
	}
	providerTurnID := turn.ProviderTurnID
	c.sendMu.Unlock()

	// Turn-started projection also takes sendMu; never wait for it under that lock.
	if !c.awaitTurnAcknowledged(ctx, providerTurnID) {
		current, err := c.store.TurnByID(ctx, turnID)
		if err != nil || current.State.Terminal() {
			return err
		}
		return ErrNoActiveTurn
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.handoffActive() {
		return ErrControllerHandoff
	}
	c.mu.Lock()
	pending := c.pendingTurnID
	c.mu.Unlock()
	if pending != providerTurnID {
		// A completion may have promoted a human turn while we waited. Never
		// follow ownership from the old correction to its successor.
		current, err := c.store.TurnByID(ctx, turnID)
		if err != nil || current.State.Terminal() {
			return err
		}
		return ErrNoActiveTurn
	}
	if err := c.conv.Interrupt(ctx, providerTurnID); err != nil {
		if !errors.Is(err, ports.ErrChatNoActiveTurn) {
			return fmt.Errorf("interrupt workflow turn %s: %w", turnID, err)
		}
		// The provider confirms this exact turn is already over. Reconcile only
		// its durable row and primary ownership, without a queue-wide Stop cutoff.
		if err := c.store.SettleTurn(ctx, c.conversation.ID, providerTurnID, domain.TurnStateInterrupted, "", c.now()); err != nil {
			return err
		}
		c.mu.Lock()
		c.pendingTurnID = ""
		if c.ackedTurnID == providerTurnID {
			c.ackedTurnID = ""
		}
		c.state = ports.ChatControllerReady
		c.mu.Unlock()
		c.reportActivity(ctx, domain.ActivityIdle, "chat.workflow.interrupted", c.now())
		return c.drainLocked(ctx, true)
	}
	return nil
}
