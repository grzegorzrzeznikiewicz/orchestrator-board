// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).
package localworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func writeScopeManifests(t *testing.T, root string) {
	t.Helper()
	for _, scope := range []string{"demo", "backend"} {
		if err := os.MkdirAll(filepath.Join(root, scope), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, scope, "package.json"), []byte(`{"scripts":{"test":"python3 -B -m unittest discover -s . -p 'test_*.py'","check":"python3 -B -c 'import ast; ast.parse(\"pass\")'"}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func scopeRunFromJSON(t *testing.T, root string) domain.LocalWorkflowRun {
	t.Helper()
	var run domain.LocalWorkflowRun
	if err := json.Unmarshal([]byte(`{"id":"run","automatic":true,"testCommands":["cd demo && CI=1 npm run test","cd demo && npm run check"],"verificationContract":{"version":1,"scopePath":"demo","commands":["cd demo && CI=1 npm run test","cd demo && npm run check"],"basis":"User explicitly restored the original demo scope","requestId":"human-scope"}}`), &run); err != nil {
		t.Fatal(err)
	}
	run.WorkspacePath = root
	run.ExecutionOutput = "Implementation of the demo is complete"
	return run
}

func TestVerificationScopePlannerCannotExpandToOtherPackages(t *testing.T) {
	root := t.TempDir()
	writeScopeManifests(t, root)
	run := scopeRunFromJSON(t, root)
	planner := ModelPlanner{Generate: func(context.Context, domain.SessionID, string, string) (string, error) {
		return `{"profile":"implementation","rationale":"Code","commands":["cd demo && CI=1 npm run test","cd demo && npm run check","cd backend && CI=1 npm run test"],"checks":["Demo behaves correctly"]}`, nil
	}}
	if _, err := planner.Plan(t.Context(), run); err == nil {
		t.Fatal("scoped verification accepted an unrelated backend command")
	}
}

func TestVerificationScopePlannerCannotReplaceImplementationWithReport(t *testing.T) {
	root := t.TempDir()
	writeScopeManifests(t, root)
	planner := ModelPlanner{Generate: func(context.Context, domain.SessionID, string, string) (string, error) {
		return `{"profile":"report","rationale":"Only report","commands":[],"checks":["Read the earlier result"]}`, nil
	}}
	if _, err := planner.Plan(t.Context(), scopeRunFromJSON(t, root)); err == nil {
		t.Fatal("explicit implementation scope was weakened to a report")
	}
}

func TestVerificationScopePlannerDisclosesScopeWithoutOtherCandidates(t *testing.T) {
	root := t.TempDir()
	writeScopeManifests(t, root)
	planner := ModelPlanner{Generate: func(_ context.Context, _ domain.SessionID, _, prompt string) (string, error) {
		if strings.Contains(prompt, "cd backend") || !strings.Contains(prompt, "User explicitly restored the original demo scope") {
			t.Fatalf("planner lost the explicit scope: %s", prompt)
		}
		return `{"profile":"implementation","rationale":"Verify the isolated demo","commands":["cd demo && CI=1 npm run test","cd demo && npm run check"],"checks":["Demo behaves correctly"]}`, nil
	}}
	if _, err := planner.Plan(t.Context(), scopeRunFromJSON(t, root)); err != nil {
		t.Fatal(err)
	}
}

func TestVerificationScopeIgnoresUnrelatedMalformedManifests(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"package.json", `{"scripts":`},
		{"backend/package.json", `{"scripts":`},
		{"backend/pyproject.toml", `[project`},
		{"backend/composer.json", `{"scripts":`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			root := t.TempDir()
			writeScopeManifests(t, root)
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(tc.path)), []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			candidates, err := scopedVerificationCandidates(root, "demo")
			want := []verificationCandidate{{"cd demo && CI=1 npm run test", "test"}, {"cd demo && npm run check", "static"}}
			if err != nil || !reflect.DeepEqual(candidates, want) {
				t.Fatalf("unrelated manifest blocked or changed demo checks: %v %v", candidates, err)
			}
			if _, err := validateVerificationContract(root, scopeRunFromJSON(t, root).VerificationContract); err != nil {
				t.Fatalf("unrelated manifest invalidated an existing demo contract: %v", err)
			}
			if _, err := discoverChecks(root, nil); err == nil {
				t.Fatal("ordinary project discovery hid an invalid manifest")
			}
		})
	}
}

func TestVerificationScopeResumeAndPlanIgnoreUnrelatedMalformedManifests(t *testing.T) {
	f, s, request := blockedScopeFixture(t)
	for _, name := range []string{"package.json", "backend/package.json"} {
		if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(name)), []byte(`{"scripts":`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request)
	if err != nil {
		t.Fatalf("unrelated manifest blocked explicit Resume: %v", err)
	}
	planner := ModelPlanner{Generate: func(context.Context, domain.SessionID, string, string) (string, error) {
		return `{"profile":"implementation","rationale":"Scoped demo","commands":["cd demo && CI=1 npm run test","cd demo && npm run check"],"checks":["Demo behaves correctly"]}`, nil
	}}
	if _, err := planner.Plan(t.Context(), *s.Run); err != nil {
		t.Fatalf("unrelated manifest blocked resumed planning: %v", err)
	}
}

func TestVerificationScopeRejectsMalformedSelectedManifests(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"package.json", `{"scripts":`},
		{"pyproject.toml", `[project`},
		{"composer.json", `{"scripts":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeScopeManifests(t, root)
			if err := os.WriteFile(filepath.Join(root, "demo", tc.name), []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := scopedVerificationCandidates(root, "demo"); !errors.Is(err, domain.ErrLocalWorkflowInvalid) {
				t.Fatalf("invalid selected manifest was ignored: %v", err)
			}
		})
	}
}

func TestVerificationScopeDoesNotSelectExcludedOrNestedPackages(t *testing.T) {
	for _, scope := range []string{".hidden", "node_modules", "vendor", "dist", "out", "build", "coverage", "test-results", "demo/nested"} {
		t.Run(scope, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, filepath.FromSlash(scope))
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"test":"python3 -B -m unittest discover"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := scopedVerificationCandidates(root, scope); !errors.Is(err, domain.ErrLocalWorkflowInvalid) {
				t.Fatalf("unsupported package scope was selected: %v", err)
			}
		})
	}
}

func TestVerificationScopeDiscoversUnittestWithoutTrustingOpaqueWrappers(t *testing.T) {
	for _, tc := range []struct{ command, want string }{
		{`python3 -B -m unittest discover -s . -p 'test_*.py'`, "test"},
		{`python3 -m unittest discover --help`, "noop"},
		{`python3 -B -m unittest discover -h`, "noop"},
		{`python3 -B -m unittest discover || true`, "unknown"},
		{`python3 -B -m unittest discover; true`, "noop"},
		{`sh bin/test-local`, "unknown"},
	} {
		if got := checkKind(tc.command, false); got != tc.want {
			t.Errorf("%q kind=%q want=%q", tc.command, got, tc.want)
		}
	}
}

func resumeScope(t *testing.T, c *Coordinator, id domain.SessionID, version int64, request *VerificationScopeRequest) (Snapshot, error) {
	t.Helper()
	resumer, ok := any(c).(interface {
		ResumeWithVerificationScope(context.Context, domain.SessionID, int64, *int, *VerificationScopeRequest) (Snapshot, error)
	})
	if !ok {
		t.Fatal("coordinator does not support an explicit run-scoped Resume")
	}
	return resumer.ResumeWithVerificationScope(t.Context(), id, version, nil, request)
}

func blockedScopeFixture(t *testing.T) (*workflowFixture, Snapshot, VerificationScopeRequest) {
	t.Helper()
	f := setupWorkflow(t, []string{"cd backend && CI=1 npm run test", "cd backend && npm run check", "git diff --check", "docker compose config --quiet", "sh bin/regression"}, 3)
	writeScopeManifests(t, f.root)
	if err := os.WriteFile(filepath.Join(f.root, "demo", "test_value.py"), []byte("import pathlib, unittest\nclass ValueTest(unittest.TestCase):\n def test_value(self):\n  self.assertEqual(pathlib.Path('../value').read_text(), 'correct')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "scope-fixture", "Implement the isolated demo")
	if err != nil {
		t.Fatal(err)
	}
	r := *s.Run
	r.Phase, r.PlanState, r.BlockCode = domain.WorkflowBlocked, "failed", "provider_operator_reconciled"
	if ok, err := f.store.AdvanceLocalWorkflow(t.Context(), r.Version, r, nil); err != nil || !ok {
		t.Fatalf("block fixture: %v %t", err, ok)
	}
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	return f, s, VerificationScopeRequest{WorkflowID: r.ID, RequestID: "human-demo-scope", ScopePath: "demo", Basis: "User explicitly restored only the original demo checks"}
}

func TestVerificationScopeResumeRunsFreshTestsAndCorrectionWithoutChangingProject(t *testing.T) {
	f, s, request := blockedScopeFixture(t)
	projectBefore, _, _ := f.store.GetProject(t.Context(), "project")
	f.coordinator.deps.Planner = ModelPlanner{Generate: func(_ context.Context, _ domain.SessionID, _, prompt string) (string, error) {
		if strings.Contains(prompt, "cd backend") || !strings.Contains(prompt, request.Basis) {
			t.Errorf("planner received the wrong scope: %s", prompt)
		}
		return `{"profile":"implementation","rationale":"Scoped demo implementation","commands":["cd demo && CI=1 npm run test","cd demo && npm run check"],"checks":["Demo behavior and syntax"]}`, nil
	}}
	s, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request)
	if err != nil {
		t.Fatal(err)
	}
	if s.Run.VerificationContract == nil || s.Run.VerificationContract.Basis != request.Basis || len(s.Run.TestCommands) != 2 || s.Current || s.Accepted {
		t.Fatalf("scope was not applied atomically: %+v", s.Run)
	}
	s = driveAutomaticReady(t, f, s.Run.ID)
	if !s.Current || s.Accepted || s.Run.Cycle != 2 || f.worker.fixes != 1 || len(s.Tests) != 3 || len(s.Reviews) != 1 {
		t.Fatalf("real scoped failure did not require a fresh correction/test/review: %+v", s)
	}
	if s.Tests[0].Status != "failed" || s.Tests[1].Status != "succeeded" || s.Tests[1].OperationID == s.Tests[0].OperationID {
		t.Fatalf("missing real test history: %+v", s.Tests)
	}
	projectAfter, _, _ := f.store.GetProject(t.Context(), "project")
	if !reflect.DeepEqual(projectBefore, projectAfter) || len(projectAfter.Config.TestCommands) != 5 {
		t.Fatal("scope changed the project contract")
	}
	next, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "new-ordinary-task", "New project work")
	if err != nil || next.Run.VerificationContract != nil || len(next.Run.TestCommands) != 5 {
		t.Fatalf("new task inherited the old exception: %+v %v", next.Run, err)
	}
}

func TestVerificationScopeRejectsStaleWrongWorkflowAndUncertainClaims(t *testing.T) {
	for _, scenario := range []string{"stale", "wrong_workflow", "uncertain"} {
		t.Run(scenario, func(t *testing.T) {
			f, s, request := blockedScopeFixture(t)
			version := s.Run.Version
			if scenario == "stale" {
				version--
			}
			if scenario == "wrong_workflow" {
				request.WorkflowID = "another-run"
			}
			if scenario == "uncertain" {
				r := *s.Run
				r.BlockCode, r.PlanState = "provider_execution_uncertain", "running"
				if _, err := f.store.AdvanceLocalWorkflow(t.Context(), r.Version, r, nil); err != nil {
					t.Fatal(err)
				}
				version++
			}
			if _, err := resumeScope(t, f.coordinator, f.session.ID, version, &request); !errors.Is(err, domain.ErrLocalWorkflowConflict) {
				t.Fatalf("unsafe scope accepted: %v", err)
			}
			after, _ := f.coordinator.Get(t.Context(), f.session.ID)
			if after.Run.VerificationContract != nil || after.Run.Phase != domain.WorkflowBlocked || after.Run.Version != version && scenario != "stale" {
				t.Fatalf("rejected scope mutated the workflow: %+v", after.Run)
			}
		})
	}
}

func TestVerificationScopeRejectsUnsafePathsAndOpaqueTests(t *testing.T) {
	f, s, request := blockedScopeFixture(t)
	for _, path := range []string{"", ".", "../demo", "/tmp/demo", "demo/", "demo/.", "demo/../demo", "missing"} {
		request.ScopePath = path
		if _, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request); !errors.Is(err, domain.ErrLocalWorkflowInvalid) {
			t.Errorf("scope %q accepted: %v", path, err)
		}
	}
	if err := os.Symlink(filepath.Join(f.root, "demo"), filepath.Join(f.root, "alias")); err != nil {
		t.Fatal(err)
	}
	request.ScopePath = "alias"
	if _, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request); !errors.Is(err, domain.ErrLocalWorkflowInvalid) {
		t.Fatal("scope symlink accepted")
	}
	for _, script := range []string{"echo tests passed", "sh bin/test-local", "python3 -m unittest discover || true", "python3 -m unittest discover --help"} {
		if err := os.WriteFile(filepath.Join(f.root, "demo", "package.json"), []byte(`{"scripts":{"test":`+string(mustScopeJSON(t, script))+`}}`), 0600); err != nil {
			t.Fatal(err)
		}
		request.ScopePath = "demo"
		if _, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request); !errors.Is(err, domain.ErrLocalWorkflowInvalid) {
			t.Errorf("non-test manifest %q accepted: %v", script, err)
		}
	}
}

func mustScopeJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestVerificationScopePersistsAcrossRecoveryAndRefreshesOnlyExecutionConfig(t *testing.T) {
	f, s, request := blockedScopeFixture(t)
	s, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request)
	if err != nil {
		t.Fatal(err)
	}
	r := *s.Run
	r.Phase, r.PlanState, r.BlockCode = domain.WorkflowBlocked, "failed", "provider_exhausted"
	if _, err := f.store.AdvanceLocalWorkflow(t.Context(), r.Version, r, nil); err != nil {
		t.Fatal(err)
	}
	project, _, _ := f.store.GetProject(t.Context(), "project")
	project.Config.Env = map[string]string{"CURRENT_SCOPE_CONFIG": "current"}
	project.Config.Reviewers[0].AgentConfig.Model = "new-review-model"
	project.Config.TestCommands = append(project.Config.TestCommands, "another project regression")
	if err := f.store.UpsertProject(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	c := replaceProviderCoordinator(t, f)
	if err := c.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	s, _ = c.Get(t.Context(), f.session.ID)
	s, err = c.Resume(t.Context(), f.session.ID, s.Run.Version, nil)
	if err != nil || s.Run.VerificationContract == nil || len(s.Run.TestCommands) != 2 {
		t.Fatalf("Resume lost the persisted scope: %+v %v", s.Run, err)
	}
	r, err = c.refreshProviderConfiguration(t.Context(), *s.Run)
	if err != nil || len(r.TestCommands) != 2 || !reflect.DeepEqual(r.Env, project.Config.Env) || r.ReviewerConfig != project.Config.Reviewers[0].AgentConfig {
		t.Fatalf("scope prevented current execution config or reverted commands: %+v %v", r, err)
	}
	r.VerificationPlan = &domain.LocalVerificationPlan{Profile: "implementation", Rationale: "Scoped", Commands: slices.Clone(r.TestCommands), Checks: []string{"Demo works"}}
	identity, err := c.providerContextID(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	changed := *r.VerificationContract
	changed.RequestID = "new-human-selection"
	r.VerificationContract = &changed
	newIdentity, err := c.providerContextID(t.Context(), r)
	if err != nil || identity == newIdentity {
		t.Fatalf("provider context lost contract identity: %q %q %v", identity, newIdentity, err)
	}
}

func TestVerificationScopeRejectsMalformedHumanBasisAndRequestIdentity(t *testing.T) {
	f, s, original := blockedScopeFixture(t)
	for _, tc := range []struct{ request, basis string }{
		{"", "Explicit human scope"},
		{" ", "Explicit human scope"},
		{"has spaces", "Explicit human scope"},
		{"has\nline", "Explicit human scope"},
		{strings.Repeat("x", 129), "Explicit human scope"},
		{"valid-request", " \n "},
		{"valid-request", strings.Repeat("x", 2049)},
		{"valid-request", "contains\x00NUL"},
	} {
		request := original
		request.RequestID, request.Basis = tc.request, tc.basis
		if _, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request); !errors.Is(err, domain.ErrLocalWorkflowInvalid) {
			t.Fatalf("malformed scope identity or basis accepted: request=%q err=%v", tc.request, err)
		}
	}
}

func TestVerificationScopeReplayCannotChangeInputs(t *testing.T) {
	f, s, request := blockedScopeFixture(t)
	s, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request)
	if err != nil {
		t.Fatal(err)
	}
	r := *s.Run
	r.Phase = domain.WorkflowBlocked
	if _, err := f.store.AdvanceLocalWorkflow(t.Context(), r.Version, r, nil); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"basis", "scope"} {
		changed := request
		if change == "basis" {
			changed.Basis = "Different requested scope"
		} else {
			changed.ScopePath = "backend"
		}
		if _, err := resumeScope(t, f.coordinator, f.session.ID, r.Version+1, &changed); !errors.Is(err, domain.ErrLocalWorkflowConflict) {
			t.Fatalf("replayed scope changed %s: %v", change, err)
		}
	}
}

func TestVerificationScopeCoordinatorRejectsAPlanOutsideTheContract(t *testing.T) {
	for _, profile := range []string{"report", "outside_scope", "omitted_check"} {
		t.Run(profile, func(t *testing.T) {
			f, s, request := blockedScopeFixture(t)
			f.coordinator.deps.Planner = plannerFunc(func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
				plan := domain.LocalVerificationPlan{Profile: "implementation", Rationale: "Planner result", Commands: []string{"cd demo && CI=1 npm run test", "cd demo && npm run check"}, Checks: []string{"Demo works"}}
				switch profile {
				case "report":
					plan.Profile, plan.Commands = "report", nil
				case "outside_scope":
					plan.Commands = append(plan.Commands, "cd backend && CI=1 npm run test")
				case "omitted_check":
					plan.Commands = plan.Commands[:1]
				}
				return plan, nil
			})
			s, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
				t.Fatal(err)
			}
			awaitProviderPlannerExit(t, f.coordinator, s.Run.ID)
			s, _ = f.coordinator.Get(t.Context(), f.session.ID)
			if s.Run.Phase != domain.WorkflowBlocked || s.Run.BlockCode != "verification_plan" || s.Run.VerificationPlan != nil || len(s.Operations) != 0 {
				t.Fatalf("unvalidated plan became executable: %+v", s.Run)
			}
		})
	}
}

func TestVerificationScopeFreshEvidenceDoesNotReuseEarlierHumanAcceptance(t *testing.T) {
	f, s, request := blockedScopeFixture(t)
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	f.coordinator.deps.Planner = ModelPlanner{Generate: func(context.Context, domain.SessionID, string, string) (string, error) {
		return `{"profile":"implementation","rationale":"Scoped implementation","commands":["cd demo && CI=1 npm run test","cd demo && npm run check"],"checks":["Demo works"]}`, nil
	}}
	s, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request)
	if err != nil {
		t.Fatal(err)
	}
	s = driveAutomaticReady(t, f, s.Run.ID)
	oldTest := latestOperation(s.Operations, "tests", s.Run.Cycle)
	oldReview := latestOperation(s.Operations, "review", s.Run.Cycle)
	f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { return nil })
	s, err = f.coordinator.Decide(t.Context(), f.session.ID, DecisionRequest{RequestID: "first-human-acceptance", IntentID: "first-desktop-intent", WorkflowID: s.Run.ID, ExpectedVersion: s.Run.Version, Kind: "accepted", SnapshotID: oldTest.SnapshotID, TestOperationID: oldTest.ID, ReviewRunID: oldReview.ReviewRunID})
	if err != nil || !s.Accepted {
		t.Fatalf("first explicit decision failed: %v", err)
	}
	s, err = f.coordinator.Stop(t.Context(), f.session.ID, s.Run.Version)
	if err != nil {
		t.Fatal(err)
	}
	s, err = f.coordinator.Resume(t.Context(), f.session.ID, s.Run.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	s = driveAutomaticReady(t, f, s.Run.ID)
	newTest := latestOperation(s.Operations, "tests", s.Run.Cycle)
	newReview := latestOperation(s.Operations, "review", s.Run.Cycle)
	if !s.Current || s.Accepted || s.Stage != "awaiting_acceptance" || len(s.Decisions) != 1 || newTest.ID == oldTest.ID || newReview.ReviewRunID == oldReview.ReviewRunID || newReview.InputOperationID != newTest.ID {
		t.Fatalf("fresh scoped verification reused old acceptance: %+v", s)
	}
}

func TestVerificationScopeInvalidPlanDoesNotLoopBecauseProjectHasMoreChecks(t *testing.T) {
	f, s, request := blockedScopeFixture(t)
	s, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request)
	if err != nil {
		t.Fatal(err)
	}
	r := *s.Run
	r.Phase, r.PlanState, r.BlockCode = domain.WorkflowBlocked, "failed", "verification_plan"
	if _, err := f.store.AdvanceLocalWorkflow(t.Context(), r.Version, r, nil); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := f.coordinator.Evaluate(t.Context(), r.ID); err != nil {
			t.Fatal(err)
		}
	}
	s, _ = f.coordinator.Get(t.Context(), f.session.ID)
	if s.Run.Version != r.Version+1 || s.Run.Phase != domain.WorkflowBlocked || len(s.Run.TestCommands) != 2 {
		t.Fatalf("project checks caused repeated unrequested scope replanning: %+v", s.Run)
	}
}

func TestVerificationScopeChangedChecksRemainActionablyBlockedWithoutPollingWrites(t *testing.T) {
	f, s, request := blockedScopeFixture(t)
	s, err := resumeScope(t, f.coordinator, f.session.ID, s.Run.Version, &request)
	if err != nil {
		t.Fatal(err)
	}
	r := *s.Run
	r.Phase, r.PlanState, r.BlockCode = domain.WorkflowBlocked, "failed", "configuration"
	if _, err := f.store.AdvanceLocalWorkflow(t.Context(), r.Version, r, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "demo", "package.json"), []byte(`{"scripts":{"test":"true"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := f.coordinator.Evaluate(t.Context(), r.ID); err != nil {
			t.Fatal(err)
		}
	}
	s, _ = f.coordinator.Get(t.Context(), f.session.ID)
	if s.Run.Version != r.Version+2 || s.Run.Phase != domain.WorkflowBlocked || s.Run.BlockCode != "verification_scope" || s.Current || s.Accepted {
		t.Fatalf("changed scope was hidden or retried: %+v", s.Run)
	}
}
