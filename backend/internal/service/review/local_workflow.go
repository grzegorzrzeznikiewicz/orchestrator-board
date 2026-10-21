// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package review

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
)

// TriggerWorkflowLocal starts a reviewer pinned to the tested workflow snapshot.
func (s *Service) TriggerWorkflowLocal(ctx context.Context, run domain.LocalWorkflowRun, operationID, expectedSnapshotID string) (domain.ReviewRun, error) {
	result, e := s.engine.TriggerWithOptions(ctx, run.SessionID, reviewcore.TriggerOptions{Harness: domain.ReviewerClaudeCode, Config: run.ReviewerConfig, Source: domain.ReviewTriggerAuto, Rerun: true, WorkflowOperationID: operationID, ExpectedSnapshotID: expectedSnapshotID, WorkflowRun: &run})
	return result.Run, e
}

// GetWorkflowLocal reads a local reviewer result and checks its current observation.
func (s *Service) GetWorkflowLocal(ctx context.Context, id string) (domain.ReviewRun, error) {
	run, ok, e := s.store.GetReviewRun(ctx, id)
	if e != nil {
		return run, e
	}
	if !ok {
		if lookup, available := s.store.(interface {
			GetWorkflowReviewRun(context.Context, string) (domain.ReviewRun, bool, error)
		}); available {
			run, ok, e = lookup.GetWorkflowReviewRun(ctx, id)
			if e != nil {
				return run, e
			}
		}
	}
	if !ok {
		return domain.ReviewRun{}, nil
	}
	if run.TargetKind != domain.ReviewTargetLocal {
		return domain.ReviewRun{}, fmt.Errorf("%w: workflow cannot use PR review", ErrConflict)
	}
	if s.engine.ValidateLocalRun(ctx, run) == nil {
		run.Current = true
	}
	return run, nil
}

// CancelWorkflowLocal cancels the reviewer associated with a workflow operation.
func (s *Service) CancelWorkflowLocal(ctx context.Context, id string) error {
	run, e := s.GetWorkflowLocal(ctx, id)
	if e != nil || run.ID == "" {
		return e
	}
	_, e = s.engine.Cancel(ctx, run.SessionID)
	return e
}
