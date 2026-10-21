// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package claudecode

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type diagnosticLaunchAgent struct {
	captureAgent
	cwd string
	env map[string]string
}

func (a *diagnosticLaunchAgent) ValidateLaunchAuthWithDiagnostics(_ context.Context, cwd string, env map[string]string) (ports.AgentAuthStatus, []domain.AgentAuthDiagnostic, error) {
	a.cwd, a.env = cwd, env
	return ports.AgentAuthStatusAuthorized, []domain.AgentAuthDiagnostic{{Method: "provider_validation", Code: "provider_accepted", HTTPStatus: 200}}, nil
}

func TestReviewProviderValidationUsesExactLaunchCredentialContext(t *testing.T) {
	agent := &diagnosticLaunchAgent{}
	r := &Reviewer{agent: agent}
	validator, ok := any(r).(ports.ReviewerLaunchAuthValidator)
	if !ok {
		t.Fatal("Claude reviewer must expose exact-context provider validation")
	}
	status, steps, err := validator.ValidateReviewLaunchAuth(t.Context(), "/review-snapshot", map[string]string{"ANTHROPIC_API_KEY": "project-key"})
	if err != nil || status != ports.AgentAuthStatusAuthorized || len(steps) != 1 || steps[0].HTTPStatus != 200 || agent.cwd != "/review-snapshot" || agent.env["ANTHROPIC_API_KEY"] != "project-key" {
		t.Fatalf("wrong provider context or lost proof: status%s steps%+v err%v", status, steps, err)
	}
	if len(agent.prelaunch) != 0 || len(agent.hooks) != 0 || len(agent.got.AllowedTools) != 0 {
		t.Fatal("provider validation launched or configured a reviewer")
	}
}
