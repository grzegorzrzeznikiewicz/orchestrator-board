// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).
package localworkflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestPlannerRejectsCompositeNonTests(t *testing.T) {
	for _, script := range []string{
		"echo TODO && true", "tsc --noEmit && eslint .", "printf passed; :",
		"echo TODO\ntrue", "false || echo passed", "echo 'vitest run && passed'",
		"echo TODO | cat", "node scripts/custom.js", "sh scripts/test.sh",
	} {
		t.Run(script, func(t *testing.T) {
			root := t.TempDir()
			writePlannerManifest(t, root, "package.json", map[string]any{"scripts": map[string]string{"test": script}})
			if _, err := configuredOnlyPlanner([]string{"CI=1 npm run test"}).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root}); err == nil {
				t.Fatal("static, no-op, or unverified script supplied implementation test evidence")
			}
		})
	}
}

func TestPlannerRejectsSkippedOrMaskedDiscoveredTests(t *testing.T) {
	for _, script := range []string{
		"false && vitest run; true", "true || vitest run", "vitest run; true",
		"vitest run\nprintf passed", "exit 0 && vitest run", "exec true && vitest run",
		"sh scripts/unknown.sh && vitest run",
	} {
		t.Run(script, func(t *testing.T) {
			root := t.TempDir()
			writePlannerManifest(t, root, "package.json", map[string]any{"scripts": map[string]string{"test": script}})
			if _, err := configuredOnlyPlanner([]string{"CI=1 npm run test"}).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root}); err == nil {
				t.Fatal("script can succeed without executing and passing its tests")
			}
		})
	}
}

func TestPlannerPreservesDiscoveredTestsWithFailurePropagation(t *testing.T) {
	for _, script := range []string{"echo running && vitest run", "npm run lint && vitest run", "vitest run && echo passed", "echo running; vitest run"} {
		t.Run(script, func(t *testing.T) {
			root := t.TempDir()
			writePlannerManifest(t, root, "package.json", map[string]any{"scripts": map[string]string{"test": script}})
			if _, err := configuredOnlyPlanner([]string{"CI=1 npm run test"}).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root}); err != nil {
				t.Fatalf("script success requires tests to run and pass: %v", err)
			}
		})
	}
}

func TestPlannerPreservesConfiguredCompositeTestsAndCustomWrappers(t *testing.T) {
	for _, command := range []string{"echo 'Running tests' && vitest run", "npm run lint && vitest run", "cd backend-py && sh bin/test-local"} {
		t.Run(command, func(t *testing.T) {
			if _, err := configuredOnlyPlanner([]string{command}).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: t.TempDir(), TestCommands: []string{command}}); err != nil {
				t.Fatalf("configured real test or declared custom wrapper was rejected: %v", err)
			}
		})
	}
}

func TestPlannerRejectsConfiguredCompositeNonTests(t *testing.T) {
	for _, command := range []string{"echo TODO && true", "printf passed; :", "tsc --noEmit && eslint .", "echo passed;", "echo passed\n"} {
		t.Run(command, func(t *testing.T) {
			if _, err := configuredOnlyPlanner([]string{command}).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: t.TempDir(), TestCommands: []string{command}}); err == nil {
				t.Fatal("configured no-op or static-only composite supplied test evidence")
			}
		})
	}
}

func TestPlannerConfiguredChecksCoverOnlyTheirScopeAndStaticTool(t *testing.T) {
	root := t.TempDir()
	writePlannerPythonProject(t, root, "backend-py", true)
	configured := []string{
		"cd backend-py && sh bin/test-local",
		"cd backend-py && ./.venv/bin/python -m ruff check --no-cache .",
		"git diff --check",
		"docker compose -f docker-compose.yml config --quiet",
		"docker compose -f backend-py/docker-compose.yml config --quiet",
	}
	plan, err := configuredOnlyPlanner(configured).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root, TestCommands: configured})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Commands, configured) {
		t.Fatalf("configured full-scope controls were duplicated: got %v, want %v", plan.Commands, configured)
	}
	checks, err := discoverChecks(root, configured)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 5 {
		t.Fatalf("configured scope should expose exactly five checks, got %+v", checks)
	}
}

func TestPlannerDiscoveryFillsDistinctToolsAndScopes(t *testing.T) {
	root := t.TempDir()
	writePlannerPythonProject(t, root, "backend-py", false)
	writePlannerPythonProject(t, root, "other-py", false)
	configured := []string{"cd 'backend-py' && sh bin/test-local", "cd ./backend-py && python3 -m ruff check --no-cache ."}
	checks, err := discoverChecks(root, configured)
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"cd backend-py && python3 -m pytest", "cd backend-py && python3 -m ruff check ."} {
		if slices.ContainsFunc(checks, func(c verificationCandidate) bool { return c.Command == missing }) {
			t.Fatalf("configured coverage left a duplicate: %+v", checks)
		}
	}
	for _, wanted := range []verificationCandidate{
		{"cd backend-py && python3 -m mypy .", "static"},
		{"cd other-py && python3 -m pytest", "test"},
		{"cd other-py && python3 -m ruff check .", "static"},
		{"cd other-py && python3 -m mypy .", "static"},
	} {
		if !slices.Contains(checks, wanted) {
			t.Fatalf("discovery dropped a distinct control %v: %+v", wanted, checks)
		}
	}
	plan, err := configuredOnlyPlanner(configured).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root, TestCommands: configured})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.Commands, "cd backend-py && python3 -m mypy .") || slices.Contains(plan.Commands, "cd other-py && python3 -m mypy .") {
		t.Fatalf("static checks were not limited to the tested scope: %+v", plan)
	}
}

func TestPlannerRetainsStaticDiscoveryForNarrowOrUncertainCoverage(t *testing.T) {
	for _, command := range []string{
		"cd backend-py && python3 -m ruff check --no-cache app tests",
		"cd backend-py && python3 -m ruff check --config custom.toml .",
		"cd backend-py && RUFF_CONFIG=custom.toml python3 -m ruff check .",
		"echo preparing && cd backend-py && python3 -m ruff check .",
		"echo preparing && cd backend-py && sh bin/test-local",
	} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			writePlannerPythonProject(t, root, "backend-py", false)
			checks, err := discoverChecks(root, []string{command})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(checks, verificationCandidate{"cd backend-py && python3 -m ruff check .", "static"}) || !slices.Contains(checks, verificationCandidate{"cd backend-py && python3 -m pytest", "test"}) {
				t.Fatalf("narrow or uncertain configured coverage removed discovered controls: %+v", checks)
			}
		})
	}
}

func TestPlannerRetainsFullTestsForNarrowConfiguredRunners(t *testing.T) {
	for _, command := range []string{
		"pytest tests/unit/test_smoke.py", "python3 -m pytest tests/unit/test_smoke.py",
		"python3 -m pytest -k smoke", "python3 -m pytest -m unit", "python3 -m pytest --ignore tests/integration",
		"PYTEST_ADDOPTS='-k smoke' python3 -m pytest", "env PYTEST_ADDOPTS='-k smoke' pytest",
	} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			writePlannerPythonProject(t, root, "backend-py", false)
			checks, err := discoverChecks(root, []string{"cd backend-py && " + command})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(checks, verificationCandidate{"cd backend-py && python3 -m pytest", "test"}) {
				t.Fatalf("narrow configured runner removed the full test command: %+v", checks)
			}
		})
	}
}

func TestPlannerBroadStandardRunnerCoversOnlyTheSameTool(t *testing.T) {
	for _, command := range []string{"pytest", "python3 -m pytest .", "python3 -m pytest -q"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			writePlannerPythonProject(t, root, "", false)
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600); err != nil {
				t.Fatal(err)
			}
			checks, err := discoverChecks(root, []string{command})
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(checks, verificationCandidate{"python3 -m pytest", "test"}) || !slices.Contains(checks, verificationCandidate{"go test ./...", "test"}) {
				t.Fatalf("broad Python coverage suppressed a distinct test tool: %+v", checks)
			}
		})
	}
}

func TestPlannerRejectsSuppressedAndOmittedConfiguredCommands(t *testing.T) {
	root := t.TempDir()
	writePlannerPythonProject(t, root, "backend-py", false)
	configured := []string{"cd backend-py && sh bin/test-local", "git diff --check"}
	for _, selected := range [][]string{
		{configured[0]},
		{configured[0], configured[1], "cd backend-py && python3 -m pytest"},
	} {
		if _, err := configuredOnlyPlanner(selected).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root, TestCommands: configured}); err == nil {
			t.Fatalf("invalid selection was accepted: %v", selected)
		}
	}
}

func TestPlannerManifestOnlyDiscoveryStillRequiresTestsAndAddsStaticChecks(t *testing.T) {
	root := t.TempDir()
	writePlannerPythonProject(t, root, "", false)
	plan, err := configuredOnlyPlanner([]string{"python3 -m pytest"}).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Commands, []string{"python3 -m pytest", "python3 -m ruff check .", "python3 -m mypy ."}) {
		t.Fatalf("manifest-only project lost test/static discovery: %+v", plan)
	}
}

func configuredOnlyPlanner(commands []string) ModelPlanner {
	return ModelPlanner{Generate: func(context.Context, domain.SessionID, string, string) (string, error) {
		answer, err := json.Marshal(domain.LocalVerificationPlan{Profile: "implementation", Rationale: "Code changed", Commands: commands, Checks: []string{"Regression coverage"}})
		return string(answer), err
	}}
}

func writePlannerManifest(t *testing.T, root, name string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, name), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func writePlannerPythonProject(t *testing.T, root, dir string, virtualenv bool) {
	t.Helper()
	path := filepath.Join(root, dir)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "pyproject.toml"), []byte("[project]\nname='example'\n[project.optional-dependencies]\ndev=['pytest>=8', 'ruff>=0.9', 'mypy>=1']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if virtualenv {
		if err := os.MkdirAll(filepath.Join(path, ".venv", "bin"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, ".venv", "bin", "python"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		// The five configured controls cover Ruff, so this fixture has no Mypy.
		if err := os.WriteFile(filepath.Join(path, "pyproject.toml"), []byte("[project]\nname='example'\n[project.optional-dependencies]\ndev=['pytest>=8', 'ruff>=0.9']\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
