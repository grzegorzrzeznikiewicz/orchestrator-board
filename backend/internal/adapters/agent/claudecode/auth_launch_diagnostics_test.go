// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package claudecode

import (
	"context"
	"net/http"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestLaunchDiagnosticsBypassCacheAndRetainNativeHTTPStatus(t *testing.T) {
	clearClaudeCredentialEnv(t)
	InvalidateAuthCache()
	t.Cleanup(InvalidateAuthCache)
	requests := 0
	server := withStubValidator(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-test"}]}`))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	})
	previous := claudeAuthCommand
	claudeAuthCommand = func(context.Context, string, string, map[string]string) (claudeCommandOutput, error) {
		return claudeCommandOutput{Stdout: []byte(`{"loggedIn":true,"apiProvider":"gateway"}`)}, nil
	}
	t.Cleanup(func() { claudeAuthCommand = previous })
	p := &Plugin{resolvedBinary: "/fixture/claude"}
	validator, ok := any(p).(interface {
		ValidateLaunchAuthWithDiagnostics(context.Context, string, map[string]string) (ports.AgentAuthStatus, []domain.AgentAuthDiagnostic, error)
	})
	if !ok {
		t.Fatal("launch validation must expose fresh native diagnostics for controlled recovery")
	}
	env := map[string]string{"ANTHROPIC_BASE_URL": server.URL, "ANTHROPIC_API_KEY": "private-key"}
	status, err := p.authStatusFor(t.Context(), "/exact-workspace", env, false)
	if err != nil || status != ports.AgentAuthStatusAuthorized || requests != 1 {
		t.Fatalf("cache setup: %s %v count%d", status, err, requests)
	}
	_, steps, err := validator.ValidateLaunchAuthWithDiagnostics(t.Context(), "/exact-workspace", env)
	if err != nil || requests != 2 {
		t.Fatalf("fresh provider request missing: %v requests%d", err, requests)
	}
	found := false
	for _, step := range steps {
		if step.Method == "provider_validation" && step.HTTPStatus == 429 && !step.Cached {
			found = true
		}
	}
	if !found {
		t.Fatalf("native status was lost: %+v", steps)
	}
}
