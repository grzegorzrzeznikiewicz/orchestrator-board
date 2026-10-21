// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localsnapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestObservedGuardIsCheapAndNeverRevivesClosedObservation(t *testing.T) {
	root := repo(t)
	m, err := Capture(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Watch(root, m)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	observer, ok := any(g).(interface{ CheckObserved() error })
	if !ok {
		t.Fatal("display-only observation requires a cheap check instead of hashing the full repository")
	}
	t.Setenv("PATH", filepath.Join(t.TempDir(), "no-programs"))
	if err := observer.CheckObserved(); err != nil {
		t.Fatalf("unchanged observation required Git or failed: %v", err)
	}
	g.Close()
	if err := observer.CheckObserved(); !errors.Is(err, ErrChanged) {
		t.Fatalf("closed observation is not current: %v", err)
	}
}

func TestGuardSnapshotValidationChecksIdentityAndFreshBytes(t *testing.T) {
	root := repo(t)
	m, err := Capture(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Watch(root, m)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	validator, ok := any(g).(interface {
		CheckSnapshot(context.Context, string, string, string) error
	})
	if !ok {
		t.Fatal("review validation needs a single fresh capture bound to its guard identity")
	}
	if err := validator.CheckSnapshot(t.Context(), root, m.ID, m.ObservationID); err != nil {
		t.Fatal(err)
	}
	if err := validator.CheckSnapshot(t.Context(), root, "other-snapshot", m.ObservationID); !errors.Is(err, ErrChanged) {
		t.Fatalf("wrong snapshot accepted: %v", err)
	}
	if err := validator.CheckSnapshot(t.Context(), root, m.ID, "other-observation"); !errors.Is(err, ErrChanged) {
		t.Fatalf("wrong observation accepted: %v", err)
	}
	if err := validator.CheckSnapshot(t.Context(), t.TempDir(), m.ID, m.ObservationID); !errors.Is(err, ErrChanged) {
		t.Fatalf("different workspace accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "fresh-check.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validator.CheckSnapshot(t.Context(), root, m.ID, m.ObservationID); !errors.Is(err, ErrChanged) {
		t.Fatalf("changed source accepted: %v", err)
	}
}
