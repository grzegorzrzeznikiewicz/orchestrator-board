// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// RecordSessionInteraction validates cooperative sender identity in the same
// transaction as its narrow write. It never modifies human-only prompt facts.
func (s *Store) RecordSessionInteraction(ctx context.Context, id domain.SessionID, sender string, at time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "record session interaction", func(q *gen.Queries) error {
		return recordSessionInteraction(ctx, q, id, sender, at)
	})
}

func recordSessionInteraction(ctx context.Context, q *gen.Queries, id domain.SessionID, sender string, at time.Time) error {
	if sender != "" {
		source, err := q.GetSession(ctx, domain.SessionID(sender))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := q.GetSession(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if source.IsTerminated || source.Kind != domain.KindOrchestrator || target.Kind != domain.KindWorker ||
			source.ProjectID == nil || target.ProjectID == nil || *source.ProjectID == "" || *source.ProjectID != *target.ProjectID || source.ID == target.ID {
			return nil
		}
	}
	_, err := q.RecordSessionInteraction(ctx, gen.RecordSessionInteractionParams{ID: id, InteractionAt: timeToNullTime(at.UTC())})
	return err
}

// Only an AO-persisted accepted steer activity qualifies; other activities do
// not advance recency. Explicit human guidance also requests local verification.
// Role/project come from sessions, never the detail labels.
func recordSteerInteraction(ctx context.Context, q *gen.Queries, conversationID, providerTurnID string, activity domain.ConversationActivity, at time.Time) error {
	var detail struct {
		Event           string               `json:"event"`
		Origin          domain.MessageOrigin `json:"origin"`
		SenderSessionID string               `json:"senderSessionId"`
		Text            string               `json:"text"`
		ClientMessageID string               `json:"clientMessageId"`
	}
	parsed := json.Unmarshal(activity.Detail, &detail) == nil
	if !parsed || detail.Event != "steer" ||
		(detail.SenderSessionID == "" && detail.Origin != domain.MessageOriginHuman) {
		return nil
	}
	conversation, err := q.SelectConversationByID(ctx, conversationID)
	if err != nil {
		return err
	}
	if conversation.SessionID == nil {
		return nil
	}
	if err := recordSessionInteraction(ctx, q, *conversation.SessionID, detail.SenderSessionID, at); err != nil {
		return err
	}
	if detail.Origin != domain.MessageOriginHuman || strings.TrimSpace(detail.Text) == "" ||
		conversation.Scope != domain.ConversationScopeSession || conversation.ReviewID.Valid || providerTurnID == "" {
		return nil
	}
	turn, err := q.SelectConversationTurnByProviderID(ctx, gen.SelectConversationTurnByProviderIDParams{ConversationID: conversationID, ProviderTurnID: providerTurnID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if turn.HandledBySessionID != *conversation.SessionID || turn.HandledByReviewID.Valid {
		return nil
	}
	identity := "activity:" + activity.ID
	if detail.ClientMessageID != "" {
		identity = "client:" + detail.ClientMessageID
	}
	return enqueueLocalWorkflowIntent(ctx, q, *conversation.SessionID, domain.LocalWorkflowIntent{
		RequestID: domain.LocalWorkflowRequestID("steer", conversationID+"\x00"+identity),
		Prompt:    detail.Text, TurnID: turn.ID, CreatedAt: at,
	})
}
