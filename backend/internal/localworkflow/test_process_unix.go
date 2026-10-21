// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

//go:build !windows

package localworkflow

import (
	"context"
	"os/exec"

	"github.com/aoagents/agent-orchestrator/backend/internal/process"
)

func testShellCommand(ctx context.Context, command string) *exec.Cmd {
	return process.CommandContext(ctx, "/bin/sh", "-c", command)
}
