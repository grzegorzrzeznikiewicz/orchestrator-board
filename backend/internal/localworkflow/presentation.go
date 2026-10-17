package localworkflow

import "github.com/aoagents/agent-orchestrator/backend/internal/domain"

// workflowStage derives board placement from execution evidence. User acceptance
// is a separate durable decision; reviewer approval never implies acceptance.
func workflowStage(s Snapshot) string {
	if s.Run == nil {
		return "executing"
	}
	switch s.Run.Phase {
	case domain.WorkflowWaiting:
		return "executing"
	case domain.WorkflowTesting:
		return "testing"
	case domain.WorkflowReviewing:
		return "local_review"
	case domain.WorkflowFixing:
		return "fixing"
	case domain.WorkflowReady:
		if s.Accepted {
			return "accepted"
		}
		if s.Current {
			return "awaiting_acceptance"
		}
		return "local_review"
	}
	if s.Run.BlockCode == "round_limit" {
		return "fixing"
	}
	// Blocks and stops stay in the stage where work needs attention.
	if len(s.Operations) > 0 {
		switch s.Operations[len(s.Operations)-1].Kind {
		case "tests":
			return "testing"
		case "review":
			return "local_review"
		case "fix":
			return "fixing"
		}
	}
	return "executing"
}
