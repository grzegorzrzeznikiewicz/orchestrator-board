//go:build windows

package localworkflow

import (
	"context"
	"os"
	"os/exec"

	"github.com/aoagents/agent-orchestrator/backend/internal/process"
)

func testShellCommand(ctx context.Context, command string) *exec.Cmd {
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		shell = "cmd.exe"
	}
	return process.CommandContext(ctx, shell, "/d", "/s", "/c", command)
}
