package localsnapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGuardRejectsEditAndRevert(t *testing.T) {
	root := repo(t)
	m, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Watch(root, m)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	write(t, root, "file.txt", "modified\n")
	write(t, root, "file.txt", "original\n")
	if id(t, root) != m.ID {
		t.Fatal("fixture did not restore identical content")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !g.Changed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !g.Changed() {
		t.Fatal("edit/revert was not observed")
	}
	if err := g.Check(context.Background()); err == nil {
		t.Fatal("changed review was accepted")
	}
}

func TestObservationSurvivesRestartAndRejectsRevertedEdits(t *testing.T) {
	root := repo(t)
	before, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if before.ID != after.ID || before.ObservationID != after.ObservationID {
		t.Fatal("read-only capture changed observation")
	}
	write(t, root, "file.txt", "modified\n")
	write(t, root, "file.txt", "original\n")
	after, err = Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if before.ID != after.ID || before.ObservationID == after.ObservationID {
		t.Fatal("persisted observation did not detect reverted writes")
	}
}

func TestGuardWatchesEmptyDirectoriesAndIgnoresUnrelatedFiles(t *testing.T) {
	root := repo(t)
	if err := os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, root, ".gitignore", "ignored.log\n")
	write(t, root, "ignored.log", "initial")
	m, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Watch(root, m)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	write(t, root, "ignored.log", "noise")
	write(t, root, ".git/index.lock", "noise")
	time.Sleep(100 * time.Millisecond)
	if g.Changed() {
		t.Fatal("ignored file or incidental Git lock invalidated review")
	}
	write(t, root, "empty/transient.txt", "new")
	if err := os.Remove(filepath.Join(root, "empty/transient.txt")); err != nil {
		t.Fatal(err)
	}
	if err := g.Check(context.Background()); err == nil {
		t.Fatal("transient file in existing empty directory was missed")
	}
	select {
	case <-g.Changes():
	default:
		t.Fatal("mutation was not monotonic")
	}

}
