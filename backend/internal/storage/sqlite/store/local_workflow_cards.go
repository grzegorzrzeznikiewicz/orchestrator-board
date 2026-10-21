// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ListLocalWorkflowCardFacts loads only public checkpoint fields in one bounded
// query. Full evidence remains behind the authoritative workflow detail API.
func (s *Store) ListLocalWorkflowCardFacts(ctx context.Context, ids []domain.SessionID) ([]domain.LocalWorkflowCardFacts, error) {
	if len(ids) == 0 || len(ids) > 200 {
		return nil, fmt.Errorf("%w: provide 1 to 200 session IDs", domain.ErrLocalWorkflowInvalid)
	}
	seen := make(map[domain.SessionID]bool, len(ids))
	unique := make([]domain.SessionID, 0, len(ids))
	for _, id := range ids {
		if id == "" || strings.TrimSpace(string(id)) != string(id) || strings.ContainsAny(string(id), ",/\\") || strings.ContainsFunc(string(id), unicode.IsControl) {
			return nil, fmt.Errorf("%w: invalid session ID", domain.ErrLocalWorkflowInvalid)
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	raw, err := json.Marshal(unique)
	if err != nil {
		return nil, err
	}
	rows, err := s.qr.SelectLocalWorkflowCardFacts(ctx, string(raw))
	if err != nil {
		return nil, err
	}
	facts := make([]domain.LocalWorkflowCardFacts, 0, len(rows))
	for _, row := range rows {
		fact := domain.LocalWorkflowCardFacts{SessionID: row.SessionID, LatestOperationKind: row.LatestOperationKind, TestOperationID: row.TestOperationID, TestsPassed: int(row.TestsPassed), TestsTotal: int(row.TestsTotal), PendingIntent: row.PendingIntent != 0, ExecutorIdle: row.ExecutorIdle != 0, ReviewRunID: row.ReviewRunID, ReviewValid: row.ReviewValid != 0, RequiredCount: int(row.RequiredCount), RequiredReason: row.RequiredReason}
		if row.RunJson != "" {
			var run domain.LocalWorkflowRun
			if err := json.Unmarshal([]byte(row.RunJson), &run); err != nil {
				return nil, err
			}
			fact.Run = &run
		}
		facts = append(facts, fact)
	}
	return facts, nil
}
