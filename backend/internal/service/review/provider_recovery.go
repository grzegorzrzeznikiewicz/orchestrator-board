// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package review

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// CheckWorkflowProvider exposes the engine's no-launch recovery probe.
func (s *Service) CheckWorkflowProvider(ctx context.Context, run domain.LocalWorkflowRun) error {
	return s.engine.CheckWorkflowProvider(ctx, run)
}
