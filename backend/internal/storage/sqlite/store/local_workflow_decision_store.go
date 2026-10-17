package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// ListLocalWorkflowDecisions reads immutable decisions in insertion order.
func (s *Store) ListLocalWorkflowDecisions(ctx context.Context, workflowID string) ([]domain.LocalWorkflowDecision, error) {
	rows, err := s.qr.ListLocalWorkflowDecisions(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.LocalWorkflowDecision, 0, len(rows))
	for _, raw := range rows {
		var decision domain.LocalWorkflowDecision
		if err := json.Unmarshal([]byte(raw), &decision); err != nil {
			return nil, err
		}
		out = append(out, decision)
	}
	return out, nil
}

// LocalWorkflowDecisionByRequest resolves an uncertain caller retry.
func (s *Store) LocalWorkflowDecisionByRequest(ctx context.Context, workflowID, requestID string) (domain.LocalWorkflowDecision, bool, error) {
	raw, err := s.qr.LocalWorkflowDecisionByRequest(ctx, gen.LocalWorkflowDecisionByRequestParams{WorkflowID: workflowID, RequestID: requestID})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LocalWorkflowDecision{}, false, nil
	}
	if err != nil {
		return domain.LocalWorkflowDecision{}, false, err
	}
	var d domain.LocalWorkflowDecision
	err = json.Unmarshal([]byte(raw), &d)
	return d, err == nil, err
}

// RecordLocalWorkflowDecision atomically records a decision and its checkpoint.
// Identical retries return false without advancing the version or emitting CDC.
func (s *Store) RecordLocalWorkflowDecision(ctx context.Context, version int64, run domain.LocalWorkflowRun, d domain.LocalWorkflowDecision) (bool, error) {
	if d.WorkflowID != run.ID || d.ExpectedVersion != version || d.ID == "" || d.RequestID == "" || d.Source != "desktop_user" || (d.Kind != "accepted" && d.Kind != "changes_requested") {
		return false, fmt.Errorf("%w: invalid decision ownership or identity", domain.ErrLocalWorkflowInvalid)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	changed := false
	err := s.inTx(ctx, "record local workflow decision", func(q *gen.Queries) error {
		raw, err := q.LocalWorkflowDecisionByRequest(ctx, gen.LocalWorkflowDecisionByRequestParams{WorkflowID: run.ID, RequestID: d.RequestID})
		if err == nil {
			var previous domain.LocalWorkflowDecision
			if err := json.Unmarshal([]byte(raw), &previous); err != nil {
				return err
			}
			if !previous.SameRequest(d) {
				return fmt.Errorf("%w: decision request has different inputs", domain.ErrLocalWorkflowConflict)
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		ok, err := advanceWorkflow(ctx, q, version, run)
		if err != nil || !ok {
			return err
		}
		encoded, err := json.Marshal(d)
		if err != nil {
			return err
		}
		err = q.InsertLocalWorkflowDecision(ctx, gen.InsertLocalWorkflowDecisionParams{ID: d.ID, WorkflowID: run.ID, RequestID: d.RequestID, RecordJson: string(encoded)})
		changed = err == nil
		return err
	})
	return changed && err == nil, err
}
