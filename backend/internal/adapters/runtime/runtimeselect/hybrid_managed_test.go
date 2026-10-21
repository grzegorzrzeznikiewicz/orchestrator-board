// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package runtimeselect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/tmux"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/review"
)

type managedFakeBackend struct {
	fakeBackend
	managedErr error
}

func (f *managedFakeBackend) DestroyManaged(_ context.Context, handle ports.RuntimeHandle) error {
	f.record("destroy-managed", handle)
	return f.managedErr
}

func managedHybridCleanup(t *testing.T, r *hybridRuntime, id string) error {
	t.Helper()
	managed, ok := any(r).(ports.ManagedRuntimeDestroyer)
	if !ok {
		t.Fatal("production runtime router hides managed cleanup capability")
	}
	return managed.DestroyManaged(context.Background(), ports.RuntimeHandle{ID: id})
}

func TestHybridManagedCleanupCoversNewHostAndPrivateFallback(t *testing.T) {
	legacy, direct := &managedFakeBackend{}, &fakeBackend{}
	r := newHybridRuntime(legacy, direct, nil, "macOS")
	if err := managedHybridCleanup(t, r, "review-worker"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(legacy.calls, []string{"destroy-managed"}) || !reflect.DeepEqual(direct.calls, []string{"destroy"}) {
		t.Fatalf("cleanup legacy=%v direct=%v", legacy.calls, direct.calls)
	}
	if legacy.handles[0].ID != "review-worker" || direct.handles[0].ID != "review-worker" {
		t.Fatalf("cleanup handles legacy=%v direct=%v", legacy.handles, direct.handles)
	}
}

func TestHybridManagedCleanupRetainsPrivateProbeFailure(t *testing.T) {
	legacy := &managedFakeBackend{managedErr: ports.ErrRuntimeProbeInconclusive}
	direct := &fakeBackend{}
	r := newHybridRuntime(legacy, direct, nil, "macOS")
	if err := managedHybridCleanup(t, r, "review-worker"); !errors.Is(err, ports.ErrRuntimeProbeInconclusive) {
		t.Fatalf("private failure must remain inconclusive: %v", err)
	}
	if !reflect.DeepEqual(legacy.calls, []string{"destroy-managed"}) || len(direct.calls) != 0 {
		t.Fatalf("private failure escaped boundary: legacy=%v direct=%v", legacy.calls, direct.calls)
	}
}

func TestHybridManagedCleanupDoesNotFallbackToGeneralLegacyDestroy(t *testing.T) {
	legacy, direct := &fakeBackend{}, &fakeBackend{}
	r := newHybridRuntime(legacy, direct, nil, "macOS")
	if err := managedHybridCleanup(t, r, "review-worker"); err == nil {
		t.Fatal("unknown legacy backend must not claim private cleanup")
	}
	if len(legacy.calls) != 0 || len(direct.calls) != 0 {
		t.Fatalf("unscoped cleanup: legacy=%v direct=%v", legacy.calls, direct.calls)
	}
}

func TestHybridManagedCleanupOfVersionedHostDoesNotTouchTmux(t *testing.T) {
	legacy, direct := &managedFakeBackend{}, &fakeBackend{}
	r := newHybridRuntime(legacy, direct, nil, "macOS")
	if err := managedHybridCleanup(t, r, directHandlePrefix+"review-worker"); err != nil {
		t.Fatal(err)
	}
	if len(legacy.calls) != 0 || !reflect.DeepEqual(direct.calls, []string{"destroy"}) || direct.handles[0].ID != "review-worker" {
		t.Fatalf("versioned host cleanup legacy=%v direct=%v handles=%v", legacy.calls, direct.calls, direct.handles)
	}
}

func TestHybridManagedCleanupUnsupportedDoesNotTouchNativeHost(t *testing.T) {
	legacy := &managedFakeBackend{managedErr: ports.ErrManagedRuntimeUnsupported}
	direct := &fakeBackend{}
	r := newHybridRuntime(legacy, direct, nil, "macOS")
	if err := managedHybridCleanup(t, r, "review-worker"); !errors.Is(err, ports.ErrManagedRuntimeUnsupported) {
		t.Fatalf("unsupported namespace error=%v", err)
	}
	if !reflect.DeepEqual(legacy.calls, []string{"destroy-managed"}) || len(direct.calls) != 0 {
		t.Fatalf("unsupported cleanup had side effects legacy=%v direct=%v", legacy.calls, direct.calls)
	}
}

type localFixtureReviewer struct{}

func (localFixtureReviewer) ReviewCommand(context.Context, ports.ReviewInvocation) (ports.ReviewCommandSpec, error) {
	return ports.ReviewCommandSpec{Argv: []string{"fixture-reviewer"}}, nil
}

func (localFixtureReviewer) ReviewMessage(context.Context, ports.ReviewInvocation) (string, error) {
	return "fixture review", nil
}

func (localFixtureReviewer) Reviewer(domain.ReviewerHarness) (ports.Reviewer, bool) {
	return localFixtureReviewer{}, true
}

func TestHybridLocalReviewerWithoutPrivateNamespaceRetainsCLITeardown(t *testing.T) {
	legacy := &managedFakeBackend{managedErr: ports.ErrManagedRuntimeUnsupported}
	direct := &fakeBackend{}
	r := newHybridRuntime(legacy, direct, nil, "Linux")
	launcher := review.NewLauncher(localFixtureReviewer{}, r, t.TempDir())
	result, err := launcher.Spawn(context.Background(), review.LaunchSpec{
		RunID: "run-1", BatchID: "batch-1", ReviewSessionID: "review-1", WorkerID: "task-1", ProjectID: "fixture",
		Harness: domain.ReviewerClaudeCode, InterfaceMode: domain.ReviewerInterfaceTUI,
		WorkspacePath: t.TempDir(), TargetKind: domain.ReviewTargetLocal, SnapshotID: "fixture-snapshot",
	})
	if err != nil {
		t.Fatalf("CLI local reviewer must retain its configured teardown policy: %v", err)
	}
	if result.HandleID != directHandlePrefix+"review-task-1" {
		t.Fatalf("CLI reviewer handle=%q", result.HandleID)
	}
	if !reflect.DeepEqual(legacy.calls, []string{"destroy-managed", "destroy"}) || !reflect.DeepEqual(direct.calls, []string{"create:review-task-1"}) {
		t.Fatalf("CLI unsupported path legacy=%v direct=%v", legacy.calls, direct.calls)
	}
}

// The launcher and router are real. Only process creation is faked: a tiny
// executable models the bundled tmux protocol, and the host backend records
// Create instead of starting an agent. PATH contains no system tmux.
func TestHybridLocalReviewerFreshLaunchWithoutSystemTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bundled tmux executable fixture requires a Unix shell; pure router contracts run on Windows")
	}
	for _, fallback := range []bool{false, true} {
		name := "direct-host"
		if fallback {
			name = "private-tmux-fallback"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			workspace := t.TempDir()
			callsPath := filepath.Join(dir, "calls")
			t.Setenv("PATH", dir)
			t.Setenv("AO_TEST_TMUX_CALLS", callsPath)
			t.Setenv("AO_TEST_TMUX_CREATED", filepath.Join(dir, "created"))
			t.Setenv("AO_TEST_TMUX_WORKSPACE", workspace)
			binary := filepath.Join(dir, "bundled-tmux")
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$AO_TEST_TMUX_CALLS"
if [ "$1" != "-L" ] || [ "$2" != "ao-review-test" ]; then
  printf 'attempted user legacy socket\n' >&2
  exit 97
fi
shift 2
case "$1" in
  list-panes|kill-session)
    printf "can't find session: review-task-1\n" >&2
    exit 1;;
  set-option|has-session)
    if [ ! -f "$AO_TEST_TMUX_CREATED" ]; then
      printf "can't find session: review-task-1\n" >&2
      exit 1
    fi;;
  new-session) : > "$AO_TEST_TMUX_CREATED";;
  display-message) printf '%s\n' "$AO_TEST_TMUX_WORKSPACE";;
esac
`
			if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			legacy := tmux.New(tmux.Options{Binary: binary, SocketName: "ao-review-test"})
			direct := &fakeBackend{}
			if fallback {
				direct.createErr = errors.New("fixture host unavailable")
			}
			r := newHybridRuntime(legacy, direct, nil, "macOS")
			launcher := review.NewLauncher(localFixtureReviewer{}, r, t.TempDir())
			result, err := launcher.Spawn(context.Background(), review.LaunchSpec{
				RunID: "run-1", BatchID: "batch-1", ReviewSessionID: "review-1", WorkerID: "task-1", ProjectID: "fixture",
				Harness: domain.ReviewerClaudeCode, InterfaceMode: domain.ReviewerInterfaceTUI,
				WorkspacePath: workspace, TargetKind: domain.ReviewTargetLocal, SnapshotID: "fixture-snapshot",
			})
			if err != nil {
				t.Fatalf("fresh local reviewer without system tmux: %v", err)
			}
			want := directHandlePrefix + "review-task-1"
			if fallback {
				want = "review-task-1"
			}
			if result.HandleID != want {
				t.Fatalf("reviewer handle=%q, want %q", result.HandleID, want)
			}
			calls, err := os.ReadFile(callsPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, call := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
				if !strings.HasPrefix(call, "-L ao-review-test ") {
					t.Fatalf("command escaped private socket: %q", call)
				}
			}
			if !strings.Contains(string(calls), "kill-session") || !reflect.DeepEqual(direct.calls, []string{"destroy", "create:review-task-1"}) {
				t.Fatalf("cleanup/create calls tmux=%s direct=%v", calls, direct.calls)
			}
		})
	}
}
