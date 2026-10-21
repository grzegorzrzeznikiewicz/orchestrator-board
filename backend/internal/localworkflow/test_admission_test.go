// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).

package localworkflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// This fixture runs as the actual project command, through the host's native
// shell on each platform. Files provide deterministic admission/release latches.
func TestTestAdmissionCommandHelper(t *testing.T) {
	if os.Getenv("AO_TEST_ADMISSION_HELPER") != "1" {
		return
	}
	started := os.Getenv("AO_TEST_ADMISSION_STARTED")
	file, err := os.OpenFile(started, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.WriteFile(os.Getenv("AO_TEST_ADMISSION_FINISHED"), nil, 0o600) }()
	release := os.Getenv("AO_TEST_ADMISSION_RELEASE")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(release); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("command fixture was not released")
}

// This foreground wrapper returns while its child retains the captured output
// pipes. It makes exec.WaitDelay observable without a long sleep command.
func TestTestAdmissionBackgroundCommandHelper(t *testing.T) {
	if os.Getenv("AO_TEST_ADMISSION_HELPER") != "1" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := process.Command(executable, "-test.run=^TestTestAdmissionCommandHelper$")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Release(); err != nil {
		t.Fatal(err)
	}
}

type admissionRun struct {
	results []domain.LocalWorkflowTestResult
	err     error
}

type admissionCommand struct {
	started  string
	release  string
	finished string
	done     <-chan admissionRun
}

func startAdmissionCommand(t *testing.T, dataDir, id string) admissionCommand {
	t.Helper()
	return startAdmissionFixture(t, dataDir, id, "TestTestAdmissionCommandHelper")
}

func startAdmissionFixture(t *testing.T, dataDir, id, helper string) admissionCommand {
	t.Helper()
	work := t.TempDir()
	started, release, finished := filepath.Join(work, "started"), filepath.Join(work, "release"), filepath.Join(work, "finished")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'"
	if runtime.GOOS == "windows" {
		quoted = `"` + executable + `"`
	}
	plan := TestPlan{
		OperationID: id, WorkspacePath: work,
		Commands: []string{quoted + " -test.run=^" + helper + "$"},
		Env: map[string]string{
			"AO_TEST_ADMISSION_HELPER": "1", "AO_TEST_ADMISSION_STARTED": started, "AO_TEST_ADMISSION_RELEASE": release, "AO_TEST_ADMISSION_FINISHED": finished,
		},
		CommandTimeout: 12 * time.Second,
	}
	done := make(chan admissionRun, 1)
	settled := make(chan struct{})
	go func() {
		defer close(settled)
		results, err := NewTestRunner(dataDir).Run(context.Background(), plan)
		done <- admissionRun{results, err}
	}()
	t.Cleanup(func() {
		_ = os.WriteFile(release, nil, 0o600)
		_ = NewTestRunner(dataDir).Cancel(context.Background(), id)
		select {
		case <-settled:
		case <-time.After(4 * time.Second):
			t.Errorf("test host %s did not exit during cleanup", id)
		}
	})
	return admissionCommand{started, release, finished, done}
}

func waitAdmission(t *testing.T, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + description)
}

func admissionFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func requireAdmissionWaiting(t *testing.T, runner *FileTestRunner, id string) bool {
	t.Helper()
	waiting, ok := any(runner).(interface{ Waiting(string) bool })
	if !ok {
		t.Fatal("test runner does not report queued admission")
	}
	return waiting.Waiting(id)
}

func awaitAdmissionRun(t *testing.T, command admissionCommand) admissionRun {
	t.Helper()
	select {
	case got := <-command.done:
		return got
	case <-time.After(4 * time.Second):
		t.Fatal("test host did not settle")
		return admissionRun{}
	}
}

func releaseAdmissionCommand(t *testing.T, command admissionCommand) {
	t.Helper()
	if err := os.WriteFile(command.release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := awaitAdmissionRun(t, command); got.err != nil || len(got.results) != 1 || got.results[0].Status != "succeeded" {
		t.Fatalf("released command result=%+v err=%v", got.results, got.err)
	}
}

func fillAdmissionSlots(t *testing.T, dataDir string) (admissionCommand, admissionCommand) {
	t.Helper()
	first := startAdmissionCommand(t, dataDir, "first")
	second := startAdmissionCommand(t, dataDir, "second")
	waitAdmission(t, func() bool { return admissionFileExists(first.started) && admissionFileExists(second.started) }, "two active commands")
	return first, second
}

// Removing the shared admission gate would let the third command write its
// started file while the two independently launched hosts still own their slots.
func TestTestAdmissionLimitsIndependentRunnersAndReconcilesQueue(t *testing.T) {
	dataDir := t.TempDir()
	first, second := fillAdmissionSlots(t, dataDir)
	third := startAdmissionCommand(t, dataDir, "third")
	restarted := NewTestRunner(dataDir)
	waitAdmission(t, func() bool {
		state, err := restarted.Reconcile(context.Background(), "third")
		return err == nil && state.State == "running"
	}, "third host ownership")
	// Give an ungated helper enough time to expose its side effect before
	// asserting the negative admission boundary.
	time.Sleep(200 * time.Millisecond)
	if admissionFileExists(third.started) {
		t.Fatal("third command started while both shared slots were occupied")
	}
	if !requireAdmissionWaiting(t, restarted, "third") {
		t.Fatal("queued host is not reported as waiting")
	}
	_, err := restarted.Run(context.Background(), TestPlan{OperationID: "third", WorkspacePath: t.TempDir(), Commands: []string{"exit 0"}})
	if err == nil || !strings.Contains(err.Error(), "refusing to launch twice") {
		t.Fatalf("restarted runner duplicated a queued launch: %v", err)
	}
	releaseAdmissionCommand(t, first)
	waitAdmission(t, func() bool { return admissionFileExists(third.started) }, "third command admission")
	if requireAdmissionWaiting(t, restarted, "third") {
		t.Fatal("admitted host still reports waiting")
	}
	releaseAdmissionCommand(t, third)
	releaseAdmissionCommand(t, second)
}

func TestTestAdmissionQueuedCancellationPublishesNoCommandResults(t *testing.T) {
	dataDir := t.TempDir()
	first, second := fillAdmissionSlots(t, dataDir)
	queued := startAdmissionCommand(t, dataDir, "cancel-queued")
	restarted := NewTestRunner(dataDir)
	waitAdmission(t, func() bool { return requireAdmissionWaiting(t, restarted, "cancel-queued") }, "queued host")
	if err := restarted.Cancel(context.Background(), "cancel-queued"); err != nil {
		t.Fatal(err)
	}
	got := awaitAdmissionRun(t, queued)
	if got.err == nil || len(got.results) != 0 || admissionFileExists(queued.started) {
		t.Fatalf("queued cancellation ran a command: results=%+v err=%v", got.results, got.err)
	}
	state, err := restarted.Reconcile(context.Background(), "cancel-queued")
	if err != nil || state.State != "settled" || state.Error == "" || len(state.Results) != 0 || requireAdmissionWaiting(t, restarted, "cancel-queued") {
		t.Fatalf("queued cancellation state=%+v err=%v", state, err)
	}
	_, err = restarted.Run(context.Background(), TestPlan{OperationID: "cancel-queued", WorkspacePath: t.TempDir(), Commands: []string{"exit 0"}})
	if err == nil || admissionFileExists(queued.started) {
		t.Fatalf("cancelled operation was replayed: %v", err)
	}
	releaseAdmissionCommand(t, first)
	releaseAdmissionCommand(t, second)
}

func TestTestAdmissionHostCrashReservesCapacityUntilCompletionIsKnown(t *testing.T) {
	dataDir := t.TempDir()
	first, second := fillAdmissionSlots(t, dataDir)
	// The command can outlive its killed host, so cleanup must release and wait
	// for that independent child before its temporary workspace is removed.
	t.Cleanup(func() {
		_ = os.WriteFile(first.release, nil, 0o600)
		waitAdmission(t, func() bool { return admissionFileExists(first.finished) }, "orphaned command cleanup")
	})
	queued := startAdmissionCommand(t, dataDir, "after-crash")
	waitAdmission(t, func() bool { return requireAdmissionWaiting(t, NewTestRunner(dataDir), "after-crash") }, "queued host")
	ownerPath := filepath.Join(dataDir, "local-workflow", "tests", "first", "owner.json")
	data, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	var owner struct{ PID int }
	if err := json.Unmarshal(data, &owner); err != nil || owner.PID <= 0 {
		t.Fatalf("owner=%s err=%v", data, err)
	}
	host, err := os.FindProcess(owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Release() }()
	if err := host.Kill(); err != nil {
		t.Fatal(err)
	}
	got := awaitAdmissionRun(t, first)
	if got.err == nil || !strings.Contains(got.err.Error(), "execution uncertain") {
		t.Fatalf("crashed host outcome=%+v err=%v", got.results, got.err)
	}
	// Keep the first project command alive. Releasing its latch here would
	// conceal an admission leak caused by the host's kernel lock disappearing.
	time.Sleep(300 * time.Millisecond)
	if admissionFileExists(queued.started) {
		t.Fatal("queued command started after host crash while both project commands were still active")
	}
	got = awaitAdmissionRun(t, queued)
	if got.err == nil || !strings.Contains(got.err.Error(), "admission uncertain") || len(got.results) != 0 {
		t.Fatalf("abandoned slot was not exposed as uncertain: results=%+v err=%v", got.results, got.err)
	}
	restarted := NewTestRunner(dataDir)
	state, err := restarted.Reconcile(context.Background(), "after-crash")
	if err != nil || state.State != "settled" || !strings.Contains(state.Error, "admission uncertain") || requireAdmissionWaiting(t, restarted, "after-crash") {
		t.Fatalf("uncertain admission state=%+v err=%v", state, err)
	}
	if err := restarted.Cancel(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	retry := startAdmissionCommand(t, dataDir, "new-runner-after-crash")
	got = awaitAdmissionRun(t, retry)
	if got.err == nil || !strings.Contains(got.err.Error(), "admission uncertain") || len(got.results) != 0 || admissionFileExists(retry.started) {
		t.Fatalf("new runner reused abandoned capacity: results=%+v err=%v", got.results, got.err)
	}
	_, err = restarted.Run(context.Background(), TestPlan{OperationID: "first", WorkspacePath: t.TempDir(), Commands: []string{"exit 0"}})
	if err == nil || !strings.Contains(err.Error(), "refusing to launch twice") {
		t.Fatalf("crashed operation was replayed: %v", err)
	}
	if err := os.WriteFile(first.release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitAdmission(t, func() bool { return admissionFileExists(first.finished) }, "orphaned command cleanup")
	releaseAdmissionCommand(t, second)
	// A normally settled slot remains usable; unknown completion in the other
	// slot stays reserved even after the test has safely released its fixture.
	remaining := startAdmissionCommand(t, dataDir, "remaining-capacity")
	waitAdmission(t, func() bool { return admissionFileExists(remaining.started) }, "remaining known capacity")
	releaseAdmissionCommand(t, remaining)
}

func TestTestAdmissionOutputWaitDelayPreservesUncertainCapacity(t *testing.T) {
	dataDir := t.TempDir()
	holder := startAdmissionCommand(t, dataDir, "foreground")
	waitAdmission(t, func() bool { return admissionFileExists(holder.started) }, "foreground command")
	background := startAdmissionFixture(t, dataDir, "output-wait", "TestTestAdmissionBackgroundCommandHelper")
	t.Cleanup(func() {
		_ = os.WriteFile(background.release, nil, 0o600)
		waitAdmission(t, func() bool { return admissionFileExists(background.finished) }, "background fixture cleanup")
	})
	waitAdmission(t, func() bool { return admissionFileExists(background.started) }, "inherited-output child")
	select {
	case got := <-background.done:
		if got.err == nil || len(got.results) != 1 || got.results[0].Status != "failed" {
			t.Fatalf("output wait result=%+v err=%v", got.results, got.err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("output wait did not settle within WaitDelay")
	}
	third := startAdmissionCommand(t, dataDir, "after-output-wait")
	time.Sleep(300 * time.Millisecond)
	if admissionFileExists(third.started) {
		t.Fatal("WaitDelay result released capacity while its inherited-output child was still active")
	}
	got := awaitAdmissionRun(t, third)
	if got.err == nil || !strings.Contains(got.err.Error(), "admission uncertain") || len(got.results) != 0 {
		t.Fatalf("uncertain output wait was not reserved: results=%+v err=%v", got.results, got.err)
	}
	releaseAdmissionCommand(t, holder)
}

func TestTestAdmissionSuccessfulCancellationReleasesCapacity(t *testing.T) {
	dataDir := t.TempDir()
	first, second := fillAdmissionSlots(t, dataDir)
	third := startAdmissionCommand(t, dataDir, "after-cancel")
	waitAdmission(t, func() bool { return requireAdmissionWaiting(t, NewTestRunner(dataDir), "after-cancel") }, "queued command")
	if err := NewTestRunner(dataDir).Cancel(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	got := awaitAdmissionRun(t, first)
	if got.err == nil || len(got.results) != 1 || got.results[0].Status != "interrupted" {
		t.Fatalf("active cancellation result=%+v err=%v", got.results, got.err)
	}
	waitAdmission(t, func() bool { return admissionFileExists(third.started) }, "capacity released by successful cancellation")
	releaseAdmissionCommand(t, third)
	releaseAdmissionCommand(t, second)
}

func TestTestAdmissionDataDirectoriesAreIndependent(t *testing.T) {
	dataDir := t.TempDir()
	first, second := fillAdmissionSlots(t, dataDir)
	independent := startAdmissionCommand(t, t.TempDir(), "independent")
	waitAdmission(t, func() bool { return admissionFileExists(independent.started) }, "independent data directory admission")
	releaseAdmissionCommand(t, independent)
	releaseAdmissionCommand(t, first)
	releaseAdmissionCommand(t, second)
}

func TestTestAdmissionLockFailureDoesNotLaunchCommands(t *testing.T) {
	dataDir := t.TempDir()
	testsDir := filepath.Join(dataDir, "local-workflow", "tests")
	if err := os.MkdirAll(testsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testsDir, ".slots"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	command := startAdmissionCommand(t, dataDir, "failed-admission")
	got := awaitAdmissionRun(t, command)
	if got.err == nil || len(got.results) != 0 || admissionFileExists(command.started) {
		t.Fatalf("failed admission launched a command: results=%+v err=%v", got.results, got.err)
	}
	state, err := NewTestRunner(dataDir).Reconcile(context.Background(), "failed-admission")
	if err != nil || state.State != "settled" || state.Error == "" {
		t.Fatalf("admission failure state=%+v err=%v", state, err)
	}
	if requireAdmissionWaiting(t, NewTestRunner(dataDir), "failed-admission") {
		t.Fatalf("failed admission %s still reports waiting", state.State)
	}
}
