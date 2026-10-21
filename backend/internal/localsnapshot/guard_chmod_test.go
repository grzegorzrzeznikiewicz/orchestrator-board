// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localsnapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestGuardAllowsGitDiffCheckReadOfModifiedTrackedFile(t *testing.T) {
	root := repo(t)
	write(t, root, "file.txt", "modified source\n")
	before, err := Capture(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "file.txt")
	if !canFilterReadChmod(path) {
		t.Skip("benign read notifications are supported only on verified macOS/APFS filesystems")
	}
	beforeStat, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Watch(root, before)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	raw, err := fsnotify.NewBufferedWatcher(128)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if err := raw.Add(root); err != nil {
		t.Fatal(err)
	}
	git(t, root, "diff", "--check")
	afterStat, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Capture(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("before id=%s observation=%s stat=%s raw=%+v", before.ID, before.ObservationID, statObservation(beforeStat), beforeStat.Sys())
	t.Logf("after  id=%s observation=%s stat=%s raw=%+v", after.ID, after.ObservationID, statObservation(afterStat), afterStat.Sys())
	if before.ID != after.ID || before.ObservationID != after.ObservationID || statObservation(beforeStat) != statObservation(afterStat) {
		t.Fatal("git diff --check changed source content, identity, permissions or durable metadata; benign-read premise is not proven")
	}
	observedChmod := false
	deadline := time.NewTimer(150 * time.Millisecond)
	defer deadline.Stop()
observe:
	for {
		select {
		case event := <-raw.Events:
			t.Logf("native event: %s", event)
			if event.Name == path && event.Op == fsnotify.Chmod {
				observedChmod = true
			}
		case err := <-raw.Errors:
			t.Fatalf("native observation failed: %v", err)
		case <-deadline.C:
			break observe
		}
	}
	if !observedChmod {
		t.Fatal("native git-read CHMOD did not reproduce on this macOS fixture; do not infer a guard fix from this run")
	}
	if err := g.Check(t.Context()); err != nil {
		t.Fatalf("unchanged tracked content and durable observation must remain reviewable after git diff --check: %v", err)
	}
}

func TestGuardRejectsRealPermissionChangesIncludingRestore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission transitions are not represented by Windows chmod")
	}
	for _, restore := range []bool{false, true} {
		name := "changed-permissions"
		if restore {
			name = "permissions-restored"
		}
		t.Run(name, func(t *testing.T) {
			root := repo(t)
			before, err := Capture(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			g, err := Watch(root, before)
			if err != nil {
				t.Fatal(err)
			}
			defer g.Close()
			path := filepath.Join(root, "file.txt")
			if err := os.Chmod(path, 0o640); err != nil {
				t.Fatal(err)
			}
			if restore {
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
				after, err := Capture(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				if after.ID != before.ID {
					t.Fatalf("permission ABA must restore identical content: before=%+v after=%+v", before, after)
				}
				if canFilterReadChmod(path) && after.ObservationID == before.ObservationID {
					t.Fatal("supported precise filesystem did not record the permission ABA")
				}
			}
			select {
			case <-g.Changes():
			case <-time.After(3 * time.Second):
				t.Fatal("real permission mutation did not invalidate the native guard")
			}
			if err := g.Check(t.Context()); !errors.Is(err, ErrChanged) {
				t.Fatalf("real permission mutation must invalidate review: %v", err)
			}
		})
	}
}

func TestReadChmodCapabilityRejectsMissingPath(t *testing.T) {
	if canFilterReadChmod(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("filesystem observation failure must retain conservative notifications")
	}
}
