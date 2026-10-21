// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localworkflow

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Recover reconciles durable history without replaying uncertain work.
func (c *Coordinator) Recover(ctx context.Context) error {
	runs, e := c.deps.Store.ListOpenLocalWorkflows(ctx)
	if e != nil {
		return e
	}
	for _, r := range runs {
		unlock := c.lock(r.ID)
		if r.PlanState == "running" && (r.Phase == domain.WorkflowWaiting || r.Phase == domain.WorkflowStopping) {
			stopProviderRetry(&r, "uncertain")
			e = c.block(ctx, r, "provider_execution_uncertain", fmt.Errorf("daemon restarted during planning; previous provider termination is unconfirmed; end the affected session before starting another task"))
			unlock()
			if e != nil {
				return e
			}
			continue
		}
		if r.ProviderRetry != nil && r.ProviderRetry.State == "checking" {
			e = c.providerRetryUncertain(ctx, r)
			unlock()
			if e != nil {
				return e
			}
			continue
		}
		switch r.Phase {
		case domain.WorkflowBlocked, domain.WorkflowStopped: // Preserve durable reasons.
		case domain.WorkflowWaiting: // No external verification has started yet.
		case domain.WorkflowFixing:
			ops, err := c.deps.Store.ListLocalWorkflowOperations(ctx, r.ID)
			if err != nil {
				unlock()
				return err
			}
			input, _, err := c.fixInput(ctx, r, ops)
			if err != nil {
				unlock()
				return err
			}
			if op := latestOperation(ops, "fix", r.Cycle); op != nil && op.InputOperationID == input {
				obs, err := c.deps.Worker.Observe(ctx, r.SessionID, op.ID)
				if err != nil {
					e = c.block(ctx, r, "executor_error", err)
				} else {
					e = c.consumeFix(ctx, r, *op, obs)
				}
			} else {
				e = c.block(ctx, r, "restart", fmt.Errorf("daemon restarted before correction dispatch; explicitly resume"))
			}
		case domain.WorkflowStopping:
			ops, err := c.deps.Store.ListLocalWorkflowOperations(ctx, r.ID)
			if err != nil {
				unlock()
				return err
			}
			e = c.finishStop(ctx, r, ops)
		default:
			e = c.block(ctx, r, "restart", fmt.Errorf("daemon restarted; history restored, but test/review observation continuity was lost. Explicitly resume to verify the current files"))
		}
		unlock()
		if e != nil {
			return e
		}
	}
	return nil
}
