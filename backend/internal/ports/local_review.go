package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// LocalReviewWorkspace serializes snapshot admission with AO's worker dispatch.
// A worker delivery invalidates existing local results before reaching the agent.
type LocalReviewWorkspace interface {
	AcquireLocalReviewWorkspace(context.Context, domain.SessionID) (func(), error)
	BeginLocalReviewEditing(context.Context, domain.SessionID) error
	HasPendingLocalReviewWork(context.Context, domain.SessionID) (bool, error)
}
