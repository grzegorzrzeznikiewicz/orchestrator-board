// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// ListPendingLocalWorkflowIntents reads explicit new-work requests only. It
// deliberately never discovers work by scanning old or merely idle sessions.
func (s *Store) ListPendingLocalWorkflowIntents(ctx context.Context) ([]domain.LocalWorkflowIntent, error) {
	rows, err := s.qr.ListPendingLocalWorkflowIntents(ctx)
	if err != nil {
		return nil, err
	}
	intents := make([]domain.LocalWorkflowIntent, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, domain.LocalWorkflowIntent{SessionID: domain.SessionID(row.SessionID), RequestID: row.RequestID, Prompt: row.Prompt, TurnID: row.TurnID, State: row.State, WorkflowID: row.WorkflowID, CreatedAt: row.CreatedAt, ConsumedAt: nullTimeToTimePtr(row.ConsumedAt)})
	}
	return intents, nil
}

// ConsumeLocalWorkflowIntent settles one request once, after the coordinator
// has durably created its run or decided that this request is superseded.
func (s *Store) ConsumeLocalWorkflowIntent(ctx context.Context, id domain.SessionID, requestID, workflowID string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.ConsumeLocalWorkflowIntent(ctx, gen.ConsumeLocalWorkflowIntentParams{SessionID: string(id), RequestID: requestID, WorkflowID: workflowID, ConsumedAt: sql.NullTime{Time: now, Valid: true}})
	return rows == 1, err
}

func localWorkflowIntentEligible(ctx context.Context, q *gen.Queries, id domain.SessionID) (bool, error) {
	row, err := q.GetSession(ctx, id)
	if err != nil {
		return false, err
	}
	rec := rowToRecord(row)
	if rec.ProjectID == "" || rec.Kind != domain.KindWorker || rec.IsTaskPreparation || rec.IsTerminated {
		return false, nil
	}
	project, err := q.GetProject(ctx, rec.ProjectID)
	if err != nil {
		return false, err
	}
	return !project.ArchivedAt.Valid && unmarshalProjectConfig(project.Config).EffectiveReviewMode() == domain.ReviewTargetLocal, nil
}

// enqueueLocalWorkflowIntent shares the accepting session/message transaction.
// Only callers with an explicit new-work fact may invoke it.
func enqueueLocalWorkflowIntent(ctx context.Context, q *gen.Queries, id domain.SessionID, intent domain.LocalWorkflowIntent) error {
	if strings.TrimSpace(intent.Prompt) == "" {
		return nil
	}
	eligible, err := localWorkflowIntentEligible(ctx, q, id)
	if err != nil || !eligible {
		return err
	}
	if intent.RequestID == "" || len(intent.RequestID) > 256 || strings.ContainsAny(intent.RequestID, "\r\n\x00") {
		return fmt.Errorf("%w: invalid workflow intent request id", domain.ErrLocalWorkflowInvalid)
	}
	previous, readErr := q.GetLocalWorkflowIntent(ctx, gen.GetLocalWorkflowIntentParams{SessionID: string(id), RequestID: intent.RequestID})
	if readErr == nil {
		if previous.Prompt != intent.Prompt || previous.TurnID != intent.TurnID {
			return fmt.Errorf("%w: workflow intent request already has different inputs", domain.ErrLocalWorkflowConflict)
		}
		return nil
	}
	if !errors.Is(readErr, sql.ErrNoRows) {
		return readErr
	}
	if intent.TurnID != "" && strings.HasPrefix(intent.RequestID, "tui:") {
		// The first submitted TUI turn is delivery of the already requested spawn,
		// not a second user task. Replace its unbound request atomically with the
		// canonical accepted-turn identity, so a retried hook stays idempotent.
		unbound, readErr := q.PendingUnboundLocalWorkflowIntent(ctx, string(id))
		if readErr == nil && strings.HasPrefix(unbound.RequestID, "spawn:") {
			if _, err = q.ConsumeLocalWorkflowIntent(ctx, gen.ConsumeLocalWorkflowIntentParams{SessionID: string(id), RequestID: unbound.RequestID, ConsumedAt: sql.NullTime{Time: intent.CreatedAt, Valid: true}}); err != nil {
				return err
			}
		}
		if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
			return readErr
		}
	}
	rows, err := q.InsertLocalWorkflowIntent(ctx, gen.InsertLocalWorkflowIntentParams{SessionID: string(id), RequestID: intent.RequestID, Prompt: intent.Prompt, TurnID: intent.TurnID, CreatedAt: intent.CreatedAt})
	if err != nil || rows == 1 {
		return err
	}
	previous, err = q.GetLocalWorkflowIntent(ctx, gen.GetLocalWorkflowIntentParams{SessionID: string(id), RequestID: intent.RequestID})
	if err != nil {
		return err
	}
	if previous.Prompt != intent.Prompt || previous.TurnID != intent.TurnID {
		return fmt.Errorf("%w: workflow intent request already has different inputs", domain.ErrLocalWorkflowConflict)
	}
	return nil
}
