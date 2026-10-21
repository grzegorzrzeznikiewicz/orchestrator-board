// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localworkflow

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestWorkflowBoardStageFollowsEvidence(t *testing.T) {
	for _, tc := range []struct {
		name              string
		phase             domain.LocalWorkflowPhase
		current           bool
		block, kind, want string
	}{
		{name: "worker done still needs tests", phase: domain.WorkflowWaiting, want: "executing"},
		{name: "tests", phase: domain.WorkflowTesting, want: "testing"},
		{name: "review", phase: domain.WorkflowReviewing, want: "local_review"},
		{name: "corrections", phase: domain.WorkflowFixing, want: "fixing"},
		{name: "current approved files await human", phase: domain.WorkflowReady, current: true, want: "awaiting_acceptance"},
		{name: "stale approval", phase: domain.WorkflowReady, want: "local_review"},
		{name: "test failure", phase: domain.WorkflowBlocked, block: "tests_failed", kind: "tests", want: "testing"},
		{name: "review failure", phase: domain.WorkflowBlocked, block: "review_failed", kind: "review", want: "local_review"},
		{name: "round limit", phase: domain.WorkflowBlocked, block: "round_limit", kind: "review", want: "fixing"},
		{name: "executor failure", phase: domain.WorkflowBlocked, block: "worker_unavailable", want: "executing"},
		{name: "stopped tests", phase: domain.WorkflowStopped, kind: "tests", want: "testing"},
		{name: "stopping corrections", phase: domain.WorkflowStopping, kind: "fix", want: "fixing"},
		{name: "restart lost observation", phase: domain.WorkflowBlocked, block: "restart", kind: "review", want: "local_review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Snapshot{Run: &domain.LocalWorkflowRun{Phase: tc.phase, BlockCode: tc.block}, Current: tc.current}
			if tc.kind != "" {
				s.Operations = []domain.LocalWorkflowOperation{{Kind: tc.kind}}
			}
			if got := workflowStage(s); got != tc.want {
				t.Fatalf("stage=%s want %s", got, tc.want)
			}
		})
	}
}
