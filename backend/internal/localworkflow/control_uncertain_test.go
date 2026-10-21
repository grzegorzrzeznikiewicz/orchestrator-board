// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package localworkflow

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Losing the host's uncertainty bit would send an automatic correction while
// this real command's inherited-output child still owns the workspace.
func TestPublishedCommandTreeUncertaintyPreventsAutomaticCorrection(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'"
	if runtime.GOOS == "windows" {
		quoted = `"` + executable + `"`
	}
	command := quoted + " -test.run=^TestTestAdmissionBackgroundCommandHelper$"
	f := setupWorkflow(t, []string{command}, 3)
	latches := t.TempDir()
	started, release, finished := filepath.Join(latches, "started"), filepath.Join(latches, "release"), filepath.Join(latches, "finished")
	for key, value := range map[string]string{
		"AO_TEST_ADMISSION_HELPER": "1", "AO_TEST_ADMISSION_STARTED": started,
		"AO_TEST_ADMISSION_RELEASE": release, "AO_TEST_ADMISSION_FINISHED": finished,
	} {
		t.Setenv(key, value)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(release, nil, 0o600)
		if admissionFileExists(started) {
			waitAdmission(t, func() bool { return admissionFileExists(finished) }, "inherited-output child cleanup")
		}
	})
	f.coordinator.deps.Planner = plannerFunc(func(_ context.Context, run domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		return domain.LocalVerificationPlan{Profile: "implementation", Rationale: "Check the changed code", Commands: run.TestCommands, Checks: []string{"Regression check"}}, nil
	})
	snapshot, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "uncertain-command-tree", "Implement the fix")
	if err != nil {
		t.Fatal(err)
	}
	var testOperation *domain.LocalWorkflowOperation
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.coordinator.Evaluate(t.Context(), snapshot.Run.ID); err != nil {
			t.Fatal(err)
		}
		snapshot, err = f.coordinator.Get(t.Context(), f.session.ID)
		if err != nil {
			t.Fatal(err)
		}
		testOperation = latestOperation(snapshot.Operations, "tests", 1)
		if testOperation != nil && !f.coordinator.isActive(testOperation.ID) {
			// The callback can finish between Get and isActive. Read its durable
			// publication only after observing completion of that callback.
			snapshot, err = f.coordinator.Get(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			testOperation = latestOperation(snapshot.Operations, "tests", 1)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if testOperation == nil || f.coordinator.isActive(testOperation.ID) {
		t.Fatal("test host did not publish its result within WaitDelay")
	}
	publication, err := readHostPublication(filepath.Join(f.data, "local-workflow", "tests", testOperation.ID))
	if err != nil || !publication.CommandTreeUncertain || !admissionFileExists(started) || admissionFileExists(finished) {
		t.Fatalf("fixture did not leave a live inherited-output child: publication=%+v err=%v", publication, err)
	}
	if snapshot.Run.Phase != domain.WorkflowBlocked || testOperation.State != "uncertain" || testOperation.CompletedAt != nil || testOperation.ErrorCode != "execution_uncertain" {
		t.Errorf("live child did not fence workflow: run=%+v operation=%+v", snapshot.Run, testOperation)
	}
	if len(snapshot.Tests) != 1 || snapshot.Tests[0].Status != "failed" || snapshot.Tests[0].StdoutLog == "" || !strings.Contains(testOperation.ErrorMessage, "command tree completion uncertain") {
		t.Errorf("uncertain command lost its results or diagnostic: tests=%+v operation=%+v", snapshot.Tests, testOperation)
	}
	if err := f.coordinator.Evaluate(t.Context(), snapshot.Run.ID); err != nil {
		t.Fatal(err)
	}
	// An incorrectly admitted fix completes immediately in scenarioWorker. Wait
	// for that boundary before checking its observable workspace mutation.
	waitAdmission(t, func() bool {
		current, err := f.coordinator.Get(t.Context(), f.session.ID)
		if err != nil {
			t.Fatal(err)
		}
		fix := latestOperation(current.Operations, "fix", 1)
		return fix == nil || !f.coordinator.isActive(fix.ID)
	}, "correction admission")
	value, err := os.ReadFile(filepath.Join(f.root, "value"))
	if err != nil || string(value) != "wrong" {
		t.Fatalf("automatic correction modified the workspace while a test child survived: value=%q err=%v", value, err)
	}
	current, err := f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil || len(current.Operations) != 1 || len(current.Reviews) != 0 || current.Run.Phase != domain.WorkflowBlocked {
		t.Fatalf("automatic pipeline advanced while test execution remained uncertain: snapshot=%+v err=%v", current, err)
	}
}
