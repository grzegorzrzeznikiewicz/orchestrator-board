// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).

//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package localworkflow

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func tryTestAdmissionLock(_ *os.File) (bool, error) {
	return false, fmt.Errorf("test admission locking is unsupported on %s", runtime.GOOS)
}

func configureTestCommandCancellation(_ *exec.Cmd) {}
