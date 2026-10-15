package localsnapshot

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSnapshotIncludesStagedContentHiddenByWorkingCopy(t *testing.T) {
	root := repo(t)
	write(t, root, "file.txt", "staged only\n")
	git(t, root, "add", "file.txt")
	write(t, root, "file.txt", "original\n")
	s, err := Prepare(context.Background(), root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	patch, err := os.ReadFile(filepath.Join(s.Path, "staged.patch"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(patch, []byte("staged only")) {
		t.Fatal("staged-only content is not reviewable")
	}
}

func TestCorruptHeadIsNotAnUnbornRepository(t *testing.T) {
	root := repo(t)
	write(t, root, ".git/refs/heads/main", "not-a-commit\n")
	if _, err := Capture(context.Background(), root); err == nil {
		t.Fatal("corrupt HEAD accepted as unborn")
	}
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = root
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	write(t, root, "file.txt", "original\n")
	write(t, root, ".gitignore", "ignored\n")
	git(t, root, "add", ".")
	git(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-m", "fixture")
	return root
}
func write(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func id(t *testing.T, root string) string {
	t.Helper()
	m, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func TestContentIdentity(t *testing.T) {
	for _, change := range []string{"same size", "untracked", "deleted", "staged", "staged deletion", "rename", "binary", "executable", "symlink"} {
		t.Run(change, func(t *testing.T) {
			root := repo(t)
			before := id(t, root)
			switch change {
			case "same size":
				write(t, root, "file.txt", "modified\n")
			case "untracked":
				write(t, root, "new file.txt", "new")
			case "deleted":
				if err := os.Remove(filepath.Join(root, "file.txt")); err != nil {
					t.Fatal(err)
				}
			case "staged":
				write(t, root, "file.txt", "changed")
				git(t, root, "add", "file.txt")
			case "staged deletion":
				git(t, root, "rm", "file.txt")
			case "rename":
				git(t, root, "mv", "file.txt", "renamed.txt")
			case "binary":
				write(t, root, "binary.bin", "\x00\x01\xff")
			case "executable":
				if err := os.Chmod(filepath.Join(root, "file.txt"), 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("file.txt", filepath.Join(root, "link")); err != nil {
					t.Fatal(err)
				}
			}
			if got := id(t, root); got == before {
				t.Fatal("change did not change content identity")
			}
		})
	}
}
func TestIdentityIgnoresUntrackedIgnoredAndMtime(t *testing.T) {
	root := repo(t)
	before := id(t, root)
	write(t, root, "ignored", "secret")
	write(t, root, "file.txt", "original\n")
	if id(t, root) != before {
		t.Fatal("same content or ignored file changed identity")
	}
	write(t, root, "tracked-ignore", "one")
	git(t, root, "add", "-f", "tracked-ignore")
	write(t, root, ".gitignore", "ignored\ntracked-ignore\n")
	before = id(t, root)
	write(t, root, "tracked-ignore", "two")
	if id(t, root) == before {
		t.Fatal("ignored tracked content must count")
	}
}
func TestPrepareMakesIndependentSnapshot(t *testing.T) {
	root := repo(t)
	write(t, root, "new.txt", "new")
	want := id(t, root)
	s, err := Prepare(context.Background(), root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != want {
		t.Fatalf("snapshot id = %s, want %s", s.ID, want)
	}
	write(t, root, "file.txt", "modified\n")
	content, err := os.ReadFile(filepath.Join(s.Path, "files", "file.txt"))
	if err != nil || string(content) != "original\n" {
		t.Fatalf("snapshot = %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(s.Path, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Path, "changes.patch")); err != nil {
		t.Fatal(err)
	}
}
func TestUnbornRepository(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	write(t, root, "new.txt", "one")
	before := id(t, root)
	write(t, root, "new.txt", "two")
	if id(t, root) == before {
		t.Fatal("unborn repository content ignored")
	}
}
func TestNestedRepositoryRejected(t *testing.T) {
	root := repo(t)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init")
	write(t, nested, "hidden.txt", "hidden")
	if _, err := Capture(context.Background(), root); err == nil {
		t.Fatal("nested repository silently skipped")
	}
}
