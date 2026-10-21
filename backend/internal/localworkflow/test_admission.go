// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).

package localworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// acquireTestAdmission holds a kernel-owned slot until the host has published
// its result. A durable owner reserves capacity after an unknown host exit,
// because project commands may survive their host. Slot files are permanent:
// replacing a locked file would admit another host under the same slot name.
func acquireTestAdmission(ctx context.Context, dataDir, operationDir string) (*os.File, error) {
	slotsDir := filepath.Join(dataDir, "local-workflow", "tests", ".slots")
	if err := os.MkdirAll(slotsDir, 0o700); err != nil {
		return nil, fmt.Errorf("prepare test admission: %w", err)
	}
	waitingPath := filepath.Join(operationDir, "waiting.json")
	waiting := false
	defer func() { _ = os.Remove(waitingPath) }()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var uncertain []string
		for slot := range 2 {
			file, err := os.OpenFile(filepath.Join(slotsDir, fmt.Sprintf("%d.lock", slot)), os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				return nil, fmt.Errorf("open test admission slot: %w", err)
			}
			locked, err := tryTestAdmissionLock(file)
			if err != nil {
				_ = file.Close()
				return nil, fmt.Errorf("lock test admission slot: %w", err)
			}
			if locked {
				ownerPath := filepath.Join(slotsDir, fmt.Sprintf("%d.owner.json", slot))
				previous, err := testAdmissionUnsettledOwner(ownerPath, filepath.Dir(operationDir))
				if err != nil {
					_ = file.Close()
					return nil, err
				}
				if previous != "" {
					uncertain = append(uncertain, fmt.Sprintf("slot %d for operation %s", slot, previous))
					if err := file.Close(); err != nil {
						return nil, fmt.Errorf("close uncertain test admission slot: %w", err)
					}
					continue
				}
				if err := writeJSONFile(ownerPath, testAdmissionOwner{OperationID: filepath.Base(operationDir)}); err != nil {
					_ = file.Close()
					return nil, fmt.Errorf("reserve test admission slot: %w", err)
				}
				if err := os.Remove(waitingPath); err != nil && !os.IsNotExist(err) {
					_ = file.Close()
					return nil, fmt.Errorf("clear test admission wait: %w", err)
				}
				return file, nil
			}
			if err := file.Close(); err != nil {
				return nil, fmt.Errorf("close test admission slot: %w", err)
			}
		}
		if len(uncertain) > 0 {
			return nil, fmt.Errorf("test admission uncertain: %s completion is not confirmed; capacity remains reserved", strings.Join(uncertain, "; "))
		}
		if !waiting {
			if err := writeJSONFile(waitingPath, struct{ PID int }{os.Getpid()}); err != nil {
				return nil, fmt.Errorf("record test admission wait: %w", err)
			}
			waiting = true
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

type testAdmissionOwner struct {
	OperationID string
}

// A released kernel lock proves only that the host is gone. Reuse also needs
// its durable command completion; no PID/liveness inference can supply that.
func testAdmissionUnsettledOwner(ownerPath, testsDir string) (string, error) {
	data, err := os.ReadFile(ownerPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read test admission owner: %w", err)
	}
	var owner testAdmissionOwner
	if err := json.Unmarshal(data, &owner); err != nil || !safeOperationID(owner.OperationID) {
		return "", fmt.Errorf("test admission uncertain: invalid slot ownership")
	}
	result, err := readHostPublication(filepath.Join(testsDir, owner.OperationID))
	if os.IsNotExist(err) {
		return owner.OperationID, nil
	}
	if err != nil {
		return "", fmt.Errorf("test admission uncertain: operation %s completion cannot be read: %w", owner.OperationID, err)
	}
	if result.CommandTreeUncertain {
		return owner.OperationID, nil
	}
	return "", nil
}
