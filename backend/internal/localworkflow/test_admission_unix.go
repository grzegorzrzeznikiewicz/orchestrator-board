// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).

//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package localworkflow

import (
	"errors"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

func tryTestAdmissionLock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func configureTestCommandCancellation(_ *exec.Cmd) {}
