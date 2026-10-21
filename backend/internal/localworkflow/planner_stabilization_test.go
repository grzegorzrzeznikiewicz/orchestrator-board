// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).
package localworkflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestPlannerRejectsDiscoveredPlaceholderTests(t *testing.T) {
	for _, script := range []string{"true", ":", "echo 'TODO: add tests'", "printf 'passed'", "tsc --noEmit"} {
		t.Run(script, func(t *testing.T) {
			root := t.TempDir()
			manifest, err := json.Marshal(map[string]any{"scripts": map[string]string{"test": script}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "package.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
			planner := ModelPlanner{Generate: func(context.Context, domain.SessionID, string, string) (string, error) {
				return `{"profile":"implementation","rationale":"Code changed","commands":["CI=1 npm run test"],"checks":["Regression coverage"]}`, nil
			}}
			if _, err := planner.Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root}); err == nil {
				t.Fatal("placeholder or static-only test script supplied implementation test evidence")
			}
		})
	}
}

func TestPlannerPreservesCompositeTestScripts(t *testing.T) {
	for _, script := range []string{"echo 'Running tests' && vitest run", "npm run lint && vitest run"} {
		t.Run(script, func(t *testing.T) {
			root := t.TempDir()
			manifest, err := json.Marshal(map[string]any{"scripts": map[string]string{"test": script}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "package.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
			planner := ModelPlanner{Generate: func(context.Context, domain.SessionID, string, string) (string, error) {
				return `{"profile":"implementation","rationale":"Code changed","commands":["CI=1 npm run test"],"checks":["Regression coverage"]}`, nil
			}}
			if _, err := planner.Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root}); err != nil {
				t.Fatalf("valid composite test script rejected: %v", err)
			}
		})
	}
}
