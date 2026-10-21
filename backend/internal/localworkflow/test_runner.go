// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/process"
	"github.com/aoagents/agent-orchestrator/backend/internal/processalive"
)

// TestPlan pins the commands, workspace and environment for one test operation.
type TestPlan struct {
	OperationID    string
	WorkspacePath  string
	DataDir        string
	Commands       []string
	Env            map[string]string
	CommandTimeout time.Duration
}

// TestProcessState describes persisted test-host evidence without inferring success from liveness.
type TestProcessState struct {
	State   string
	Results []domain.LocalWorkflowTestResult
	Error   string
}

// TestRunner runs, cancels and reconciles test operations by stable identity.
type TestRunner interface {
	Run(context.Context, TestPlan) ([]domain.LocalWorkflowTestResult, error)
	Cancel(context.Context, string) error
	Reconcile(context.Context, string) (TestProcessState, error)
}

// FileTestRunner delegates commands to a helper that owns their durable result and cancellation.
type FileTestRunner struct{ dataDir string }

// NewTestRunner constructs a runner rooted in the configured AO data directory.
func NewTestRunner(dataDir string) *FileTestRunner { return &FileTestRunner{dataDir: dataDir} }
func safeOperationID(id string) bool {
	return id != "" && len(id) <= 128 && !strings.ContainsAny(id, "/\\.\x00")
}
func (r *FileTestRunner) operationDir(id string) (string, error) {
	if !safeOperationID(id) {
		return "", fmt.Errorf("invalid test operation id")
	}
	return filepath.Join(r.dataDir, "local-workflow", "tests", id), nil
}

type hostTestResult struct {
	Result domain.LocalWorkflowTestResult
	Out    string
	Err    string
}
type hostResult struct {
	Tests                []hostTestResult
	Error                string
	CommandTreeUncertain bool
}

func readHostPublication(dir string) (hostResult, error) {
	b, e := os.ReadFile(filepath.Join(dir, "result.json"))
	if e != nil {
		return hostResult{}, e
	}
	var result hostResult
	if e := json.Unmarshal(b, &result); e != nil {
		return hostResult{}, e
	}
	return result, nil
}

func readHostResult(dir string) (TestProcessState, error) {
	result, e := readHostPublication(dir)
	if e != nil {
		return TestProcessState{}, e
	}
	state := TestProcessState{State: "settled", Error: result.Error, Results: make([]domain.LocalWorkflowTestResult, 0, len(result.Tests))}
	if result.CommandTreeUncertain {
		state.State = "uncertain"
	}
	for _, v := range result.Tests {
		test := v.Result
		test.StdoutLog = v.Out
		test.StderrLog = v.Err
		state.Results = append(state.Results, test)
	}
	return state, nil
}
func writeJSONFile(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	tmp := path + ".tmp"
	f, e := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e != nil {
		_ = f.Close()
		return e
	}
	if e := f.Sync(); e != nil {
		_ = f.Close()
		return e
	}
	if e := f.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}

// Reconcile reads durable completion or reports an active or uncertain launch.
func (r *FileTestRunner) Reconcile(ctx context.Context, id string) (TestProcessState, error) {
	if e := ctx.Err(); e != nil {
		return TestProcessState{}, e
	}
	dir, e := r.operationDir(id)
	if e != nil {
		return TestProcessState{}, e
	}
	result, e := readHostResult(dir)
	if e == nil {
		return result, nil
	}
	if !errors.Is(e, os.ErrNotExist) {
		return TestProcessState{State: "uncertain"}, e
	}
	if _, e = os.Stat(filepath.Join(dir, "launch.claim")); errors.Is(e, os.ErrNotExist) {
		return TestProcessState{State: "absent"}, nil
	}
	if e != nil {
		return TestProcessState{}, e
	}
	var owner struct{ PID int }
	b, e := os.ReadFile(filepath.Join(dir, "owner.json"))
	if e == nil && json.Unmarshal(b, &owner) == nil && owner.PID > 0 && processalive.Alive(owner.PID) {
		return TestProcessState{State: "running"}, nil
	}
	return TestProcessState{State: "uncertain"}, nil
}

// Waiting reports a host that has not yet been admitted to run commands.
// Durable completion always takes precedence over a queue marker.
func (r *FileTestRunner) Waiting(id string) bool {
	dir, err := r.operationDir(id)
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "result.json")); err == nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "waiting.json"))
	return err == nil
}

// Cancel asks the owning host to cancel without signaling a recorded PID.
func (r *FileTestRunner) Cancel(ctx context.Context, id string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	dir, e := r.operationDir(id)
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, "cancel"), []byte("cancel"), 0o600)
}

// Run claims an operation once and waits for the helper-owned command results.
func (r *FileTestRunner) Run(ctx context.Context, plan TestPlan) ([]domain.LocalWorkflowTestResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(plan.Commands) == 0 {
		return nil, fmt.Errorf("no test commands configured")
	}
	dir, e := r.operationDir(plan.OperationID)
	if e != nil {
		return nil, e
	}
	for _, command := range plan.Commands {
		if strings.TrimSpace(command) == "" || strings.ContainsRune(command, 0) {
			return nil, fmt.Errorf("invalid test command")
		}
	}
	if e := os.MkdirAll(dir, 0o700); e != nil {
		return nil, e
	}
	claim, e := os.OpenFile(filepath.Join(dir, "launch.claim"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(e, os.ErrExist) {
		state, e := r.Reconcile(ctx, plan.OperationID)
		if e != nil {
			return nil, e
		}
		if state.State == "settled" {
			return testResultsOutcome(state)
		}
		if state.Error != "" {
			return state.Results, fmt.Errorf("test operation %s is %s; refusing to launch twice: %s", plan.OperationID, state.State, state.Error)
		}
		return state.Results, fmt.Errorf("test operation %s is %s; refusing to launch twice", plan.OperationID, state.State)
	}
	if e != nil {
		return nil, e
	}
	if e := claim.Close(); e != nil {
		return nil, e
	}
	plan.DataDir = r.dataDir
	if plan.CommandTimeout <= 0 {
		plan.CommandTimeout = 20 * time.Minute
	}
	if e := writeJSONFile(filepath.Join(dir, "plan.json"), plan); e != nil {
		return nil, e
	}
	executable, e := os.Executable()
	if e != nil {
		return nil, e
	}
	cmd := process.Command(executable, "--internal-local-workflow-test-host", filepath.Join(dir, "plan.json"))
	if e := cmd.Start(); e != nil {
		return nil, e
	}
	// Reap this host, but it owns cancellation and result publication even if the
	// daemon exits. Neither this daemon nor a replacement sends signals by PID.
	hostDone := make(chan error, 1)
	go func() { hostDone <- cmd.Wait() }()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	cancelled := false
	var cancelDeadline time.Time
	for {
		if result, e := readHostResult(dir); e == nil {
			return testResultsOutcome(result)
		} else if !errors.Is(e, os.ErrNotExist) {
			return nil, e
		}
		if !cancelled && ctx.Err() != nil {
			if e := r.Cancel(context.WithoutCancel(ctx), plan.OperationID); e != nil {
				return nil, e
			}
			cancelled = true
			cancelDeadline = time.Now().Add(10 * time.Second)
		}
		if cancelled && time.Now().After(cancelDeadline) {
			return nil, fmt.Errorf("test cancellation uncertain: host has not settled")
		}
		select {
		case <-ticker.C:
		case err := <-hostDone:
			if result, readErr := readHostResult(dir); readErr == nil {
				return testResultsOutcome(result)
			}
			if err != nil {
				return nil, fmt.Errorf("test host exited without publishing a result; execution uncertain: %w", err)
			}
			return nil, errors.New("test host exited without publishing a result; execution uncertain")
		}
	}
}
func testResultsOutcome(state TestProcessState) ([]domain.LocalWorkflowTestResult, error) {
	if state.Error != "" {
		return state.Results, errors.New(state.Error)
	}
	if state.State == "uncertain" {
		return state.Results, errors.New("test command tree completion uncertain")
	}
	return state.Results, nil
}
