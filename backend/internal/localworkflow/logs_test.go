package localworkflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/process"
	"github.com/aoagents/agent-orchestrator/backend/internal/processalive"
)

func TestWorkflowLogIsBoundedOwnedAndCannotEscapeDataRoot(t *testing.T) {
	f := setupWorkflow(t, []string{"dd if=/dev/zero bs=1048576 count=11 2>/dev/null; exit 7"}, 3)
	ctx := context.Background()
	if _, err := f.coordinator.Start(ctx, f.session.ID, "large-log"); err != nil {
		t.Fatal(err)
	}
	blocked := awaitPhase(t, f, domain.WorkflowBlocked)
	if len(blocked.Tests) != 1 {
		t.Fatalf("test results: %+v", blocked.Tests)
	}
	result := blocked.Tests[0]
	info, err := os.Stat(result.StdoutLog)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != maxTestLogBytes || !result.StdoutTruncated {
		t.Fatalf("unbounded log: size=%d truncated=%v", info.Size(), result.StdoutTruncated)
	}
	log, err := f.coordinator.TestLog(ctx, f.session.ID, result.ID, "stdout")
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Text) != 256*1024 || !log.Truncated {
		t.Fatalf("unbounded response: bytes=%d truncated=%v", len(log.Text), log.Truncated)
	}
	other, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: f.session.ProjectID, Kind: domain.KindWorker, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.TestLog(ctx, other.ID, result.ID, "stdout"); !errors.Is(err, domain.ErrLocalWorkflowNotFound) {
		t.Fatalf("another session read the log: %v", err)
	}
	if _, err = f.coordinator.TestLog(ctx, f.session.ID, result.ID, "../stdout"); !errors.Is(err, domain.ErrLocalWorkflowInvalid) {
		t.Fatalf("invalid stream accepted: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err = os.WriteFile(outside, []byte("must stay outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(result.StdoutLog); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, result.StdoutLog); err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.TestLog(ctx, f.session.ID, result.ID, "stdout"); err == nil {
		t.Fatal("followed a log symlink outside AO data")
	}
}

func TestLogOpenFailurePreventsCommandExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	data, work := t.TempDir(), t.TempDir()
	dir := filepath.Join(data, "local-workflow", "tests", "log-error")
	if err := os.MkdirAll(filepath.Join(dir, "0.stdout"), 0o700); err != nil {
		t.Fatal(err)
	}
	results, err := NewTestRunner(data).Run(context.Background(), TestPlan{OperationID: "log-error", WorkspacePath: work, Commands: []string{"touch should-not-run"}})
	if err == nil || len(results) != 1 || results[0].Status == "succeeded" {
		t.Fatalf("log failure accepted: %+v %v", results, err)
	}
	if _, err = os.Stat(filepath.Join(work, "should-not-run")); !os.IsNotExist(err) {
		t.Fatal("spawned command before opening logs")
	}
}

func TestCancellationNeverSignalsARecordedForeignPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
	child := process.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	runner := NewTestRunner(t.TempDir())
	dir, err := runner.operationDir("recycled-pid")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "launch.claim"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = writeJSONFile(filepath.Join(dir, "owner.json"), struct{ PID int }{child.Process.Pid}); err != nil {
		t.Fatal(err)
	}
	if err = runner.Cancel(context.Background(), "recycled-pid"); err != nil {
		t.Fatal(err)
	}
	if !processalive.Alive(child.Process.Pid) {
		t.Fatal("signalled a process using only its recorded PID")
	}
	state, err := runner.Reconcile(context.Background(), "recycled-pid")
	if err != nil || state.State != "running" {
		t.Fatalf("foreign PID did not preserve uncertainty: %+v %v", state, err)
	}
}
