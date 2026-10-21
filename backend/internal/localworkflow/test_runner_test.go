// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localworkflow

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if handled, code := RunTestHost(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func TestTestRunnerExecutesOnceAndPersistsFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; Windows execution requires its native runner checks")
	}
	ctx := context.Background()
	root := t.TempDir()
	work := filepath.Join(root, "space in path")
	if e := os.Mkdir(work, 0700); e != nil {
		t.Fatal(e)
	}
	runner := NewTestRunner(filepath.Join(root, "data"))
	plan := TestPlan{OperationID: "op-one", WorkspacePath: work, Commands: []string{"printf '%s' \"$WF_VALUE\"; printf diagnostic >&2; printf x >> counter", "exit 7", "touch should-not-exist"}, Env: map[string]string{"WF_VALUE": "hello"}, CommandTimeout: time.Second}
	results, err := runner.Run(ctx, plan)
	if err == nil || len(results) != 2 || results[1].ExitCode == nil || *results[1].ExitCode != 7 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	out, e := os.ReadFile(results[0].StdoutLog)
	if e != nil || string(out) != "hello" {
		t.Fatalf("stdout=%q %v", out, e)
	}
	diagnostic, e := os.ReadFile(results[0].StderrLog)
	if e != nil || string(diagnostic) != "diagnostic" {
		t.Fatalf("stderr=%q %v", diagnostic, e)
	}
	if _, e = os.Stat(filepath.Join(work, "should-not-exist")); !os.IsNotExist(e) {
		t.Fatal("ran after failure")
	}
	_, _ = runner.Run(ctx, plan)
	counter, e := os.ReadFile(filepath.Join(work, "counter"))
	if e != nil || string(counter) != "x" {
		t.Fatalf("duplicate process: %q %v", counter, e)
	}
	state, e := NewTestRunner(filepath.Join(root, "data")).Reconcile(ctx, "op-one")
	if e != nil || state.State != "settled" || len(state.Results) != 2 {
		t.Fatalf("reconcile=%+v %v", state, e)
	}
}

func TestTestRunnerTimeoutAndUncertainClaim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; Windows execution requires its native runner checks")
	}
	root := t.TempDir()
	runner := NewTestRunner(root)
	got, e := runner.Run(context.Background(), TestPlan{OperationID: "timeout", WorkspacePath: t.TempDir(), Commands: []string{"sleep 10"}, CommandTimeout: 100 * time.Millisecond})
	if e == nil || len(got) != 1 || got[0].Status != "timed_out" {
		t.Fatalf("timeout=%+v %v", got, e)
	}
	dir := filepath.Join(root, "local-workflow", "tests", "crash")
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "launch.claim"), []byte("claimed before spawn"), 0600); e != nil {
		t.Fatal(e)
	}
	_, e = runner.Run(context.Background(), TestPlan{OperationID: "crash", WorkspacePath: t.TempDir(), Commands: []string{"exit 0"}})
	if e == nil || !strings.Contains(e.Error(), "uncertain") {
		t.Fatalf("uncertain launch was replayed: %v", e)
	}
	if _, e = runner.Run(context.Background(), TestPlan{OperationID: "../escape", Commands: []string{"exit 0"}}); e == nil {
		t.Fatal("escaped data root")
	}
	if _, e = runner.Run(context.Background(), TestPlan{OperationID: "empty"}); e == nil {
		t.Fatal("empty test plan succeeded")
	}
}

func TestTestRunnerCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; Windows execution requires its native runner checks")
	}
	runner := NewTestRunner(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, e := runner.Run(ctx, TestPlan{OperationID: "cancel", WorkspacePath: t.TempDir(), Commands: []string{"sleep 10"}})
		done <- e
	}()
	time.AfterFunc(150*time.Millisecond, cancel)
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process did not stop")
	}
	state, e := runner.Reconcile(context.Background(), "cancel")
	if e != nil || state.State != "settled" || len(state.Results) != 1 || state.Results[0].Status != "interrupted" {
		t.Fatalf("cancel result=%+v %v", state, e)
	}
}
