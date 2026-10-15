// Package localsnapshot captures exact Git working contents without modifying
// the source index, creating commits, or creating another Git worktree.
package localsnapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// ErrChanged means a snapshot could not be captured consistently.
var ErrChanged = errors.New("workspace changed while preparing local review")

// Entry records actual bytes and permissions, including explicit missing paths.
type Entry struct {
	Path    string `json:"path"`
	Mode    uint32 `json:"mode"`
	Hash    string `json:"hash"`
	Missing bool   `json:"missing,omitempty"`
	Link    string `json:"link,omitempty"`
}

// Manifest is a versioned, content-addressed description of a local review.
type Manifest struct {
	Version       int     `json:"version"`
	Root          string  `json:"root"`
	Head          string  `json:"head"`
	Index         string  `json:"index"`
	Files         []Entry `json:"files"`
	ID            string  `json:"id"`
	ObservationID string  `json:"observationId"`
}

// Snapshot is an independent copy plus its immutable manifest.
type Snapshot struct {
	ID            string
	Path          string
	ObservationID string
}

func output(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := process.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", root}, args...)...)
	literal := "1"
	if len(args) > 0 && args[0] == "check-ignore" {
		literal = "0"
	}
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS="+literal)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}
func digest(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }

// Capture hashes all tracked files and nonignored new files, the index and HEAD.
// No size/statistics fingerprint or truncated diff is used as content identity.
func Capture(ctx context.Context, root string) (Manifest, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Manifest{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Manifest{}, err
	}
	top, err := output(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return Manifest{}, err
	}
	if filepath.Clean(strings.TrimSpace(string(top))) != abs {
		return Manifest{}, errors.New("local review requires the repository root")
	}
	head, _ := output(ctx, abs, "rev-parse", "--verify", "HEAD")
	if len(head) == 0 { // Only a genuinely unborn HEAD may omit a commit.
		if _, e := output(ctx, abs, "symbolic-ref", "HEAD"); e != nil {
			return Manifest{}, fmt.Errorf("resolve HEAD: %w", e)
		}
	}
	index, err := output(ctx, abs, "ls-files", "--stage", "-z")
	if err != nil {
		return Manifest{}, err
	}
	for _, line := range bytes.Split(index, []byte{0}) {
		if len(line) == 0 {
			continue
		}
		meta, _, ok := bytes.Cut(line, []byte{'\t'})
		if !ok {
			return Manifest{}, errors.New("invalid index entry")
		}
		fields := strings.Fields(string(meta))
		if len(fields) != 3 || fields[0] == "160000" || fields[2] != "0" {
			return Manifest{}, errors.New("local review does not support submodules or an unmerged index")
		}
	}
	listed, err := output(ctx, abs, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Manifest{}, err
	}
	paths := map[string]bool{}
	for _, p := range bytes.Split(listed, []byte{0}) {
		if len(p) > 0 {
			paths[string(p)] = true
		}
	}
	if len(head) > 0 {
		tree, e := output(ctx, abs, "ls-tree", "-r", "--name-only", "-z", "HEAD")
		if e != nil {
			return Manifest{}, e
		}
		for _, p := range bytes.Split(tree, []byte{0}) {
			if len(p) > 0 {
				paths[string(p)] = true
			}
		}
	}
	ordered := make([]string, 0, len(paths))
	for p := range paths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	m := Manifest{Version: 1, Root: abs, Head: strings.TrimSpace(string(head)), Index: digest(index), Files: make([]Entry, 0, len(paths))}
	for _, p := range ordered {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		e, err := readEntry(abs, p, nil)
		if err != nil {
			return Manifest{}, err
		}
		m.Files = append(m.Files, e)
	}
	endIndex, err := output(ctx, abs, "ls-files", "--stage", "-z")
	if err != nil {
		return Manifest{}, err
	}
	endHead, _ := output(ctx, abs, "rev-parse", "--verify", "HEAD")
	endListed, err := output(ctx, abs, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Manifest{}, err
	}
	if !bytes.Equal(index, endIndex) || !bytes.Equal(head, endHead) || !bytes.Equal(listed, endListed) {
		return Manifest{}, ErrChanged
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return Manifest{}, err
	}
	m.ID = digest(raw)
	var observation strings.Builder
	observation.WriteString(m.ID)
	for _, entry := range m.Files {
		if entry.Missing {
			continue
		}
		st, e := os.Lstat(filepath.Join(m.Root, filepath.FromSlash(entry.Path)))
		if e != nil {
			return Manifest{}, ErrChanged
		}
		fmt.Fprintf(&observation, "\x00%s:%d:%d", entry.Path, st.ModTime().UnixNano(), st.Size())
		// ctime and inode supplement mtime without including access time, which
		// our own read would change. Available field names vary across platforms.
		v := reflect.Indirect(reflect.ValueOf(st.Sys()))
		if v.IsValid() && v.Kind() == reflect.Struct {
			for _, key := range []string{"Ino", "Ctim", "Ctimespec"} {
				f := v.FieldByName(key)
				if f.IsValid() && f.CanInterface() {
					fmt.Fprintf(&observation, ":%v", f.Interface())
				}
			}
		}
	}
	m.ObservationID = digest([]byte(observation.String()))
	return m, nil
}

func readEntry(root, path string, dst io.Writer) (Entry, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean != filepath.FromSlash(path) {
		return Entry{}, fmt.Errorf("unsupported snapshot path %q", path)
	}
	abs := filepath.Join(root, clean)
	// Resolve parents only; a symlink must never redirect a tracked file read.
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Entry{Path: path, Missing: true}, nil
		}
		return Entry{}, err
	}
	if parent != root && !strings.HasPrefix(parent, root+string(filepath.Separator)) {
		return Entry{}, errors.New("snapshot path escapes repository")
	}
	st, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return Entry{Path: path, Missing: true}, nil
	}
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Path: path, Mode: uint32(st.Mode())}
	switch {
	case st.Mode()&os.ModeSymlink != 0:
		e.Link, err = os.Readlink(abs)
		if err != nil {
			return Entry{}, err
		}
		e.Hash = digest([]byte(e.Link))
		// The snapshot never follows the link; represent its target as text.
		if dst != nil {
			_, err = io.WriteString(dst, e.Link)
		}
	case st.Mode().IsRegular():
		f, openErr := os.Open(abs)
		if openErr != nil {
			return Entry{}, openErr
		}
		h := sha256.New()
		w := io.Writer(h)
		if dst != nil {
			w = io.MultiWriter(h, dst)
		}
		_, err = io.Copy(w, f)
		after, statErr := f.Stat()
		closeErr := f.Close()
		if err != nil {
			return Entry{}, err
		}
		if statErr != nil {
			return Entry{}, statErr
		}
		if closeErr != nil {
			return Entry{}, closeErr
		}
		if !os.SameFile(st, after) || st.Size() != after.Size() || !st.ModTime().Equal(after.ModTime()) || st.Mode() != after.Mode() {
			return Entry{}, ErrChanged
		}
		e.Hash = hex.EncodeToString(h.Sum(nil))
	default:
		return Entry{}, fmt.Errorf("local review cannot snapshot directory, nested repository or special file %q", path)
	}
	if err != nil {
		return Entry{}, err
	}
	end, err := os.Lstat(abs)
	if err != nil {
		return Entry{}, ErrChanged
	}
	if !os.SameFile(st, end) || !st.ModTime().Equal(end.ModTime()) || st.Mode() != end.Mode() {
		return Entry{}, ErrChanged
	}
	return e, nil
}

// Prepare copies the manifest's exact contents and verifies source/copy agreement.
// The returned directory contains files/, manifest.json and changes.patch.
func Prepare(ctx context.Context, root, dest string) (snapshot Snapshot, err error) {
	before, err := Capture(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return Snapshot{}, err
	}
	dir, err := os.MkdirTemp(dest, "snapshot-")
	if err != nil {
		return Snapshot{}, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	if err := os.Mkdir(filepath.Join(dir, "files"), 0o700); err != nil {
		return Snapshot{}, err
	}
	for _, entry := range before.Files {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		if entry.Missing {
			continue
		}
		target := filepath.Join(dir, "files", filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return Snapshot{}, err
		}
		f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if e != nil {
			return Snapshot{}, e
		}
		copied, e := readEntry(before.Root, entry.Path, f)
		closeErr := f.Close()
		if e != nil {
			return Snapshot{}, e
		}
		if closeErr != nil {
			return Snapshot{}, closeErr
		}
		if copied != entry {
			return Snapshot{}, ErrChanged
		}
		if err := os.Chmod(target, 0o400); err != nil {
			return Snapshot{}, err
		}
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--binary"}
	if before.Head != "" {
		args = append(args, "HEAD")
	}
	args = append(args, "--")
	for _, patch := range []struct {
		name string
		args []string
	}{
		{"changes.patch", args},
		{"staged.patch", []string{"diff", "--cached", "--no-ext-diff", "--no-textconv", "--binary", "--"}},
		{"unstaged.patch", []string{"diff", "--no-ext-diff", "--no-textconv", "--binary", "--"}},
	} {
		file, openErr := os.OpenFile(filepath.Join(dir, patch.name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
		if openErr != nil {
			return Snapshot{}, openErr
		}
		cmd := process.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", before.Root}, patch.args...)...)
		cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS=1")
		cmd.Stdout = file
		runErr := cmd.Run()
		closeErr := file.Close()
		if runErr != nil {
			return Snapshot{}, fmt.Errorf("capture %s: %w", patch.name, runErr)
		}
		if closeErr != nil {
			return Snapshot{}, closeErr
		}
	}
	after, err := Capture(ctx, before.Root)
	if err != nil {
		return Snapshot{}, err
	}
	if before.ID != after.ID || before.ObservationID != after.ObservationID {
		return Snapshot{}, ErrChanged
	}
	raw, err := json.MarshalIndent(before, "", "  ")
	if err != nil {
		return Snapshot{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o400); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ID: before.ID, Path: dir, ObservationID: before.ObservationID}, nil
}

// statObservation excludes access time: reading a snapshot must not invalidate it.
func statObservation(st os.FileInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d:%d:%d", st.ModTime().UnixNano(), st.Size(), st.Mode())
	v := reflect.Indirect(reflect.ValueOf(st.Sys()))
	if v.IsValid() && v.Kind() == reflect.Struct {
		for _, key := range []string{"Ino", "Ctim", "Ctimespec"} {
			f := v.FieldByName(key)
			if f.IsValid() && f.CanInterface() {
				fmt.Fprintf(&b, ":%v", f.Interface())
			}
		}
	}
	return b.String()
}
