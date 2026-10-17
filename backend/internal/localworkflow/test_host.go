package localworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/process"
)

const maxTestLogBytes = 10 * 1024 * 1024

// RunTestHost is a private executable mode, outside the public thin HTTP CLI.
// A claim survives a crash, so a missing result can never cause a second launch.
func RunTestHost(args []string) (bool, int) {
	if len(args) == 0 || args[0] != "--internal-local-workflow-test-host" {
		return false, 0
	}
	if len(args) != 2 {
		return true, 2
	}
	data, e := os.ReadFile(args[1])
	if e != nil {
		return true, 1
	}
	var plan TestPlan
	if json.Unmarshal(data, &plan) != nil || !safeOperationID(plan.OperationID) || len(plan.Commands) == 0 {
		return true, 2
	}
	r := NewTestRunner(plan.DataDir)
	dir, e := r.operationDir(plan.OperationID)
	if e != nil {
		return true, 2
	}
	expected, e := filepath.Abs(filepath.Join(dir, "plan.json"))
	if e != nil {
		return true, 2
	}
	actual, e := filepath.Abs(args[1])
	if e != nil || actual != expected {
		return true, 2
	}
	claim, e := os.OpenFile(filepath.Join(dir, "run.claim"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if e != nil {
		return true, 1
	}
	if claim.Close() != nil {
		return true, 1
	}
	if writeJSONFile(filepath.Join(dir, "owner.json"), struct{ PID int }{os.Getpid()}) != nil {
		return true, 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if _, e := os.Stat(filepath.Join(dir, "cancel")); e == nil {
					cancel()
					return
				}
			}
		}
	}()
	result := hostResult{Tests: []hostTestResult{}}
	for i, command := range plan.Commands {
		test, e := runTestCommand(ctx, plan, dir, i, command)
		result.Tests = append(result.Tests, hostTestResult{test, test.StdoutLog, test.StderrLog})
		if e != nil {
			result.Error = e.Error()
			break
		}
	}
	if e = writeJSONFile(filepath.Join(dir, "result.json"), result); e != nil {
		return true, 1
	}
	return true, 0
}

type cappedLog struct {
	file      *os.File
	remaining int
	truncated bool
}

func (w *cappedLog) Write(b []byte) (int, error) {
	n := len(b)
	write := b
	if len(write) > w.remaining {
		write = write[:w.remaining]
		w.truncated = true
	}
	if len(write) > 0 {
		written, e := w.file.Write(write)
		w.remaining -= written
		if e != nil {
			return written, e
		}
	}
	return n, nil
}
func runTestCommand(parent context.Context, plan TestPlan, dir string, index int, command string) (domain.LocalWorkflowTestResult, error) {
	now := time.Now().UTC()
	test := domain.LocalWorkflowTestResult{ID: fmt.Sprintf("%s-%d", plan.OperationID, index), OperationID: plan.OperationID, CommandIndex: index, Command: command, Status: "failed", StartedAt: now, StdoutLog: filepath.Join(dir, fmt.Sprintf("%d.stdout", index)), StderrLog: filepath.Join(dir, fmt.Sprintf("%d.stderr", index))}
	finish := func(e error) (domain.LocalWorkflowTestResult, error) {
		at := time.Now().UTC()
		test.CompletedAt = &at
		return test, e
	}
	out, e := os.OpenFile(test.StdoutLog, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if e != nil {
		return finish(e)
	}
	defer func() { _ = out.Close() }()
	stderr, e := os.OpenFile(test.StderrLog, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if e != nil {
		return finish(e)
	}
	defer func() { _ = stderr.Close() }()
	stdoutWriter := &cappedLog{file: out, remaining: maxTestLogBytes}
	stderrWriter := &cappedLog{file: stderr, remaining: maxTestLogBytes}
	timeout := plan.CommandTimeout
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := testShellCommand(ctx, command)
	process.ConfigureTreeCancellation(cmd)
	cmd.Dir = plan.WorkspacePath
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = os.Environ()
	for key, value := range plan.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdout = io.Writer(stdoutWriter)
	cmd.Stderr = io.Writer(stderrWriter)
	e = cmd.Run()
	test.StdoutTruncated = stdoutWriter.truncated
	test.StderrTruncated = stderrWriter.truncated
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		test.ExitCode = &code
	}
	if syncErr := out.Sync(); e == nil {
		e = syncErr
	}
	if syncErr := stderr.Sync(); e == nil {
		e = syncErr
	}
	switch {
	case parent.Err() != nil:
		test.Status = "interrupted"
		e = parent.Err()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		test.Status = "timed_out"
		e = ctx.Err()
	case e == nil:
		test.Status = "succeeded"
	}
	if e != nil {
		e = fmt.Errorf("test command %d %s: %w", index+1, test.Status, e)
	}
	return finish(e)
}
