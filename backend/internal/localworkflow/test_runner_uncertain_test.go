// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package localworkflow

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// A published result records what the host observed, but cannot prove that a
// surviving command tree ended. Reconciliation and repeat Run must preserve it.
func TestTestRunnerPreservesPublishedCommandTreeUncertainty(t *testing.T) {
	for _, tc := range []struct {
		name      string
		uncertain bool
		message   string
		wantState string
	}{
		{"surviving child", true, "test command 1 failed: command tree completion uncertain: output pipes remain open", "uncertain"},
		{"missing diagnostic", true, "", "uncertain"},
		{"settled failure", false, "test command 1 failed: exit status 7", "settled"},
		{"settled success", false, "", "settled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := t.TempDir()
			dir := filepath.Join(data, "local-workflow", "tests", "published")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "launch.claim"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			want := []domain.LocalWorkflowTestResult{{ID: "published-0", OperationID: "published", Command: "check", Status: "failed", StdoutLog: filepath.Join(dir, "0.stdout"), StderrLog: filepath.Join(dir, "0.stderr")}}
			publication := hostResult{Tests: []hostTestResult{{Result: want[0], Out: want[0].StdoutLog, Err: want[0].StderrLog}}, Error: tc.message, CommandTreeUncertain: tc.uncertain}
			if err := writeJSONFile(filepath.Join(dir, "result.json"), publication); err != nil {
				t.Fatal(err)
			}
			runner := NewTestRunner(data)
			state, err := runner.Reconcile(t.Context(), "published")
			if err != nil || state.State != tc.wantState || state.Error != tc.message || !reflect.DeepEqual(state.Results, want) {
				t.Errorf("published evidence changed: state=%+v err=%v; want %s with original results and diagnostic", state, err, tc.wantState)
			}
			results, err := runner.Run(t.Context(), TestPlan{OperationID: "published", Commands: []string{"must never run"}})
			if !reflect.DeepEqual(results, want) {
				t.Errorf("repeat Run lost published results: %+v", results)
			}
			if tc.uncertain || tc.message != "" {
				if err == nil || tc.message != "" && !strings.Contains(err.Error(), tc.message) {
					t.Errorf("repeat Run lost failure or uncertainty: %v", err)
				}
			} else if err != nil {
				t.Errorf("settled success became a failure: %v", err)
			}
		})
	}
}

// Returning a publication must fail closed even if an old or incomplete host
// persisted the uncertainty bit without an accompanying error message.
func TestTestResultsOutcomeRejectsUncertainCompletion(t *testing.T) {
	want := []domain.LocalWorkflowTestResult{{ID: "published-0", Status: "succeeded"}}
	results, err := testResultsOutcome(TestProcessState{State: "uncertain", Results: want})
	if err == nil || !strings.Contains(err.Error(), "uncertain") || !reflect.DeepEqual(results, want) {
		t.Fatalf("uncertain publication looked successful or lost results: results=%+v err=%v", results, err)
	}
}

func TestPublishedCommandTreeUncertaintyFencesWorkflowControls(t *testing.T) {
	for _, action := range []string{"resume", "supersede"} {
		for _, uncertain := range []bool{true, false} {
			t.Run(action+"/"+map[bool]string{true: "uncertain", false: "settled"}[uncertain], func(t *testing.T) {
				f := setupWorkflow(t, []string{"exit 0"}, 3)
				started, err := f.coordinator.Start(t.Context(), f.session.ID, "published")
				if err != nil {
					t.Fatal(err)
				}
				op := f.coordinator.newOperation(*started.Run, nil, "tests")
				run, ok, err := f.coordinator.reserve(t.Context(), *started.Run, domain.WorkflowBlocked, op)
				if err != nil || !ok {
					t.Fatalf("reserve test operation: ok=%v err=%v", ok, err)
				}
				dir := filepath.Join(f.data, "local-workflow", "tests", op.ID)
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := writeJSONFile(filepath.Join(dir, "result.json"), hostResult{Error: "test command failed", CommandTreeUncertain: uncertain}); err != nil {
					t.Fatal(err)
				}
				if action == "resume" {
					_, err = f.coordinator.Resume(context.Background(), f.session.ID, run.Version, nil)
					if uncertain && err == nil || !uncertain && err != nil {
						t.Fatalf("Resume admission: uncertain=%v err=%v", uncertain, err)
					}
				} else {
					quiet, err := f.coordinator.supersede(t.Context(), run)
					if err != nil || quiet == uncertain {
						t.Fatalf("supersession admission: uncertain=%v quiet=%v err=%v", uncertain, quiet, err)
					}
				}
				after, err := f.coordinator.Get(t.Context(), f.session.ID)
				if err != nil {
					t.Fatal(err)
				}
				if uncertain && (after.Run.ClosedAt != nil || !activeOperation(after.Operations[0]) || len(after.Operations) != 1) {
					t.Fatalf("uncertain operation lost its fence: %+v", after)
				}
			})
		}
	}
}
