// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ListSessionLocalWorkflows preserves prior task scopes across automatic
// follow-ups while returning the newest run first.
func (s *Store) ListSessionLocalWorkflows(ctx context.Context, id domain.SessionID) ([]domain.LocalWorkflowRun, error) {
	rows, err := s.qr.ListSessionLocalWorkflows(ctx, string(id))
	if err != nil {
		return nil, err
	}
	runs := make([]domain.LocalWorkflowRun, 0, len(rows))
	for _, row := range rows {
		run, err := decodeWorkflow(row)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}
