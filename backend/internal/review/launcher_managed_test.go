// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package review

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// This runtime has a healthy private namespace, but cannot inspect the legacy
// namespace because the host has no system tmux. Creating a new reviewer must
// only clean up the private namespace in which it is about to create the pane.
type managedReviewerRuntime struct {
	fakeRuntime
	managedErr     error
	managedHandle  string
	generalDeletes int
}

func (r *managedReviewerRuntime) Destroy(_ context.Context, _ ports.RuntimeHandle) error {
	r.generalDeletes++
	return ports.ErrRuntimeProbeInconclusive
}

func (r *managedReviewerRuntime) DestroyManaged(ctx context.Context, handle ports.RuntimeHandle) error {
	r.managedHandle = handle.ID
	if r.managedErr != nil {
		return r.managedErr
	}
	return r.fakeRuntime.Destroy(ctx, handle)
}

func TestLauncherFreshReviewerUsesManagedCleanupWithoutLegacyProbe(t *testing.T) {
	rt := &managedReviewerRuntime{}
	l := newTestLauncher(t, &fakeReviewer{}, rt)
	spec := launchSpec()
	spec.TargetKind = domain.ReviewTargetLocal
	if _, err := l.Spawn(context.Background(), spec); err != nil {
		t.Fatalf("fresh private reviewer must not require legacy tmux: %v", err)
	}
	if rt.generalDeletes != 0 || rt.managedHandle != "review-mer-1" || !rt.destroyBefore || !rt.created {
		t.Fatalf("cleanup/creation = %+v", rt)
	}
}

func TestLauncherManagedCleanupFailurePreventsReviewerSpawn(t *testing.T) {
	probeErr := errors.New("private tmux connection refused")
	rt := &managedReviewerRuntime{managedErr: probeErr}
	l := newTestLauncher(t, &fakeReviewer{}, rt)
	spec := launchSpec()
	spec.TargetKind = domain.ReviewTargetLocal
	_, err := l.Spawn(context.Background(), spec)
	if !errors.Is(err, probeErr) || rt.created || rt.generalDeletes != 0 {
		t.Fatalf("Spawn err=%v, created=%v, legacy cleanup calls=%d", err, rt.created, rt.generalDeletes)
	}
}

func TestLauncherDurableTeardownRetainsGeneralCleanup(t *testing.T) {
	rt := &managedReviewerRuntime{}
	l := newTestLauncher(t, &fakeReviewer{}, rt)
	err := l.Destroy(context.Background(), "review-mer-1")
	if !errors.Is(err, ports.ErrRuntimeProbeInconclusive) || rt.generalDeletes != 1 || rt.managedHandle != "" {
		t.Fatalf("durable teardown err=%v, legacy calls=%d, managed handle=%q", err, rt.generalDeletes, rt.managedHandle)
	}
}

func TestLauncherRestoringReviewerRetainsLegacyCleanup(t *testing.T) {
	for _, restoreTerminal := range []bool{false, true} {
		name := "resumed-spawn"
		if restoreTerminal {
			name = "restore-terminal"
		}
		t.Run(name, func(t *testing.T) {
			rt := &managedReviewerRuntime{}
			l := newTestLauncher(t, &fakeReviewer{}, rt)
			spec := launchSpec()
			spec.TargetKind = domain.ReviewTargetLocal
			var err error
			if restoreTerminal {
				_, err = l.RestoreTerminal(context.Background(), spec)
			} else {
				spec.AgentSessionID = "existing-provider-session"
				_, err = l.Spawn(context.Background(), spec)
			}
			if !errors.Is(err, ports.ErrRuntimeProbeInconclusive) || rt.generalDeletes != 1 || rt.managedHandle != "" || rt.created {
				t.Fatalf("restore err=%v, legacy calls=%d, managed=%q, created=%v", err, rt.generalDeletes, rt.managedHandle, rt.created)
			}
		})
	}
}

func TestLauncherPRReplacementRetainsLegacyCleanup(t *testing.T) {
	rt := &managedReviewerRuntime{}
	l := newTestLauncher(t, &fakeReviewer{}, rt)
	_, err := l.Spawn(context.Background(), launchSpec())
	if !errors.Is(err, ports.ErrRuntimeProbeInconclusive) || rt.generalDeletes != 1 || rt.managedHandle != "" || rt.created {
		t.Fatalf("PR replacement err=%v, legacy calls=%d, managed=%q, created=%v", err, rt.generalDeletes, rt.managedHandle, rt.created)
	}
}

func TestLauncherWithoutPrivateNamespaceRetainsGeneralCleanup(t *testing.T) {
	rt := &managedReviewerRuntime{managedErr: ports.ErrManagedRuntimeUnsupported}
	l := newTestLauncher(t, &fakeReviewer{}, rt)
	spec := launchSpec()
	spec.TargetKind = domain.ReviewTargetLocal
	_, err := l.Spawn(context.Background(), spec)
	if !errors.Is(err, ports.ErrRuntimeProbeInconclusive) || rt.generalDeletes != 1 || rt.created {
		t.Fatalf("unconfigured private namespace err=%v, general calls=%d, created=%v", err, rt.generalDeletes, rt.created)
	}
}
