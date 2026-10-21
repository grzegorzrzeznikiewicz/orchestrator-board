// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package sessionmanager

import (
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestLocalWorkflowIntentSeedArmsOnlyNewLocalTerminalTasks(t *testing.T) {
	for _, name := range []string{"local-terminal", "chat-owned-by-turn", "pr", "orchestrator", "standalone", "promptless"} {
		t.Run(name, func(t *testing.T) {
			cfg := ports.SpawnConfig{ProjectID: "budget", Kind: domain.KindWorker, Harness: domain.HarnessCodex, RequestedMode: domain.SessionModeTUI, Prompt: "implement budget report"}
			project := domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal}
			switch name {
			case "chat-owned-by-turn":
				cfg.RequestedMode = domain.SessionModeChat
			case "pr":
				project.ReviewMode = domain.ReviewTargetPR
			case "orchestrator":
				cfg.Kind = domain.KindOrchestrator
			case "standalone":
				cfg.ProjectID = ""
			case "promptless":
				cfg.Prompt = ""
			}
			rec := seedRecord(cfg, project, time.Now())
			if name == "local-terminal" {
				if rec.LocalWorkflowIntent == nil || rec.LocalWorkflowIntent.Prompt != "implement budget report" || rec.LocalWorkflowIntent.RequestID == "" || rec.LocalWorkflowIntent.TurnID != "" {
					t.Fatalf("new terminal task lacks pending acceptance: %+v", rec.LocalWorkflowIntent)
				}
			} else if rec.LocalWorkflowIntent != nil {
				t.Fatalf("unintended auto workflow: %+v", rec.LocalWorkflowIntent)
			}
		})
	}
}
