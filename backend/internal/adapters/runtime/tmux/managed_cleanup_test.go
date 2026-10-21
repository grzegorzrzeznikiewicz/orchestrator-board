// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package tmux

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func destroyManagedForTest(t *testing.T, r *Runtime, id string) error {
	t.Helper()
	managed, ok := any(r).(interface {
		DestroyManaged(context.Context, ports.RuntimeHandle) error
	})
	if !ok {
		t.Fatal("tmux runtime lacks private-only reviewer cleanup")
	}
	return managed.DestroyManaged(context.Background(), ports.RuntimeHandle{ID: id})
}

func TestDestroyManagedMissingPrivateSessionNeedsNoLegacyBinary(t *testing.T) {
	r, fr := newTestRuntime(0)
	r.socketName, r.legacyBinary = "ao-private", ""
	fr.outputs = [][]byte{nil, []byte("can't find session: review-worker"), []byte("can't find session: review-worker")}
	fr.err = &exec.ExitError{}
	if err := destroyManagedForTest(t, r, "review-worker"); err != nil {
		t.Fatalf("absent private pane must allow fresh reviewer: %v", err)
	}
	assertPrivateCleanupCalls(t, fr.calls)
}

func TestDestroyManagedStalePrivateSessionDoesNotTouchCachedLegacy(t *testing.T) {
	r, fr := newTestRuntime(0)
	r.socketName, r.legacyBinary = "ao-private", "user-tmux"
	r.sessionSockets["review-worker"] = ""
	fr.outputs = [][]byte{[]byte("314\n")}
	reaper := &recordingReaper{}
	r.reapSessions = reaper.reap
	if err := destroyManagedForTest(t, r, "review-worker"); err != nil {
		t.Fatal(err)
	}
	assertPrivateCleanupCalls(t, fr.calls)
	if got, ok := r.sessionSockets["review-worker"]; !ok || got != "" {
		t.Fatalf("managed cleanup changed cached legacy routing: %q, %v", got, ok)
	}
	if !reflect.DeepEqual(reaper.pids, [][]int{{314}}) {
		t.Fatalf("private pane process cleanup = %v", reaper.pids)
	}
}

func TestDestroyManagedPrivateConnectionFailureBlocksCleanup(t *testing.T) {
	r, fr := newTestRuntime(0)
	r.socketName, r.legacyBinary = "ao-private", "user-tmux"
	fr.err = &exec.ExitError{}
	fr.outputs = [][]byte{nil, []byte("error connecting to private socket (Connection refused)")}
	err := destroyManagedForTest(t, r, "review-worker")
	if err == nil {
		t.Fatal("inconclusive private socket failure must block reviewer replacement")
	}
	if len(fr.calls) != 2 || fr.calls[1].args[2] != "set-option" {
		t.Fatalf("unexpected cleanup after private error: %#v", fr.calls)
	}
	for _, call := range fr.calls {
		if call.name != "tmux-test" || call.args[0] != "-L" || call.args[1] != "ao-private" {
			t.Fatalf("private failure escaped namespace: %#v", call)
		}
	}
}

func TestDestroyManagedWithoutPrivateNamespaceFailsClosed(t *testing.T) {
	r, fr := newTestRuntime(0)
	r.socketName = ""
	err := destroyManagedForTest(t, r, "review-worker")
	if !errors.Is(err, ports.ErrManagedRuntimeUnsupported) || len(fr.calls) != 0 {
		t.Fatalf("unscoped cleanup err=%v, calls=%#v", err, fr.calls)
	}
}

func assertPrivateCleanupCalls(t *testing.T, calls []runnerCall) {
	t.Helper()
	want := []string{"list-panes", "set-option", "kill-session"}
	if len(calls) != len(want) {
		t.Fatalf("cleanup commands = %#v", calls)
	}
	for i, call := range calls {
		if call.name != "tmux-test" || len(call.args) < 3 || call.args[0] != "-L" || call.args[1] != "ao-private" || call.args[2] != want[i] {
			t.Fatalf("cleanup must stay on bundled/private socket: %#v", call)
		}
	}
}
