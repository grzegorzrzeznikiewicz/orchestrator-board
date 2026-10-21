// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package main

import (
	"fmt"
	"os"

	"github.com/aoagents/agent-orchestrator/backend/internal/cli"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
)

func main() {
	if handled, code := localworkflow.RunTestHost(os.Args[1:]); handled {
		os.Exit(code)
	}
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ExitCode(err))
	}
}
