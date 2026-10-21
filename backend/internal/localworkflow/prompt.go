// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localworkflow

import (
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// BuildFixPrompt returns the original task and required corrections without promoting suggestions.
func BuildFixPrompt(run domain.LocalWorkflowRun, result domain.LocalReviewResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Continue the original task and satisfy its acceptance criteria.\n\n%s\n\nLatest user instruction:\n%s\n\nRequired corrections from local review of snapshot %s:\n", run.Task, run.LatestUserPrompt, result.SnapshotID)
	for _, finding := range result.Findings {
		if finding.Kind == "required" {
			fmt.Fprintf(&b, "- %s:%d: %s\n", finding.Path, finding.Line, finding.Reason)
		}
	}
	b.WriteString("\nFix the required issues and finish your turn. AO will run the configured tests and request a fresh independent review. Optional suggestions do not block this task. Leave changes uncommitted. Do not create commits, push, or create PRs/MRs. Preserve the user's other instructions and existing work.\n")
	return b.String()
}

// BuildUserFixPrompt preserves user feedback as a separate correction source.
func BuildUserFixPrompt(run domain.LocalWorkflowRun, decision domain.LocalWorkflowDecision) string {
	return fmt.Sprintf("Continue the original task and satisfy its acceptance criteria.\n\n%s\n\nLatest user instruction:\n%s\n\nUser requested corrections for snapshot %s:\n%s\n\nFinish your turn after the corrections. AO will run fresh tests and independent review. Leave changes uncommitted. Do not create commits, push, create PRs/MRs, merge or deploy. Preserve the user's other instructions and existing work.\n", run.Task, run.LatestUserPrompt, decision.SnapshotID, decision.Comment)
}
