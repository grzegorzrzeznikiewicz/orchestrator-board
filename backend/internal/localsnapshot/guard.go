package localsnapshot

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

// Guard records any mutation for the lifetime of a review. An error or a closed
// notification stream is uncertainty, so it invalidates the review too.
type Guard struct {
	watcher     *fsnotify.Watcher
	manifest    Manifest
	directories map[string]string
	changed     atomic.Bool
	closed      atomic.Bool
	once        sync.Once
	done        chan struct{}
	change      chan struct{}
}

// Watch subscribes before snapshot preparation. New files/directories mutate an
// already-watched parent; any event invalidates the pass, so recursive extension
// after that event is unnecessary. Ignored dependency directories aren't scanned.
func Watch(root string, m Manifest) (*Guard, error) {
	w, err := fsnotify.NewBufferedWatcher(8192)
	if err != nil {
		return nil, err
	}
	g := &Guard{watcher: w, manifest: m, done: make(chan struct{}), change: make(chan struct{})}
	dirs := map[string]bool{m.Root: true}
	tracked := map[string]bool{}
	trackedDirs := map[string]bool{}
	for _, entry := range m.Files {
		tracked[filepath.Join(m.Root, filepath.FromSlash(entry.Path))] = true
		for p := filepath.Dir(filepath.Join(m.Root, filepath.FromSlash(entry.Path))); p != m.Root && strings.HasPrefix(p, m.Root+string(filepath.Separator)); p = filepath.Dir(p) {
			trackedDirs[p] = true
		}
	}
	ignored := func(path string) bool {
		relative, e := filepath.Rel(m.Root, path)
		if e != nil {
			return false
		}
		_, e = output(context.Background(), m.Root, "check-ignore", "--quiet", "--", relative)
		if e == nil {
			return true
		}
		var exit *exec.ExitError
		if errors.As(e, &exit) && exit.ExitCode() == 1 {
			return false
		}
		g.markChanged()
		return false
	}
	err = filepath.WalkDir(m.Root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			return nil
		}
		if path != m.Root && (d.Name() == ".git" || (!trackedDirs[path] && ignored(path))) {
			return filepath.SkipDir
		}
		dirs[path] = true
		return nil
	})
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	g.directories = make(map[string]string, len(dirs))
	for dir := range dirs {
		st, e := os.Stat(dir)
		if e != nil {
			_ = w.Close()
			return nil, e
		}
		g.directories[dir] = statObservation(st)
	}
	gitDir, err := output(context.Background(), root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	gitRoot := strings.TrimSpace(string(gitDir))
	dirs[gitRoot] = true
	refs := filepath.Join(gitRoot, "refs")
	if _, e := os.Stat(refs); e == nil {
		err = filepath.WalkDir(refs, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				dirs[path] = true
			}
			return nil
		})
		if err != nil {
			_ = w.Close()
			return nil, err
		}
	}
	for dir := range dirs {
		if _, e := os.Stat(dir); os.IsNotExist(e) {
			continue
		}
		if err = w.Add(dir); err != nil {
			_ = w.Close()
			return nil, err
		}
	}
	relevant := func(event fsnotify.Event) bool {
		path := event.Name
		if (path == gitRoot || strings.HasPrefix(path, gitRoot+string(filepath.Separator))) && event.Op == fsnotify.Chmod {
			return false
		}
		if path == gitRoot {
			return true
		}
		if strings.HasPrefix(path, gitRoot+string(filepath.Separator)) {
			rel, _ := filepath.Rel(gitRoot, path)
			if strings.HasSuffix(rel, ".lock") {
				return false
			}
			return rel == "HEAD" || rel == "index" || rel == "packed-refs" || rel == "config" || rel == "shallow" || rel == filepath.Join("info", "exclude") || strings.HasPrefix(rel, "refs"+string(filepath.Separator))
		}
		return tracked[path] || !ignored(path)
	}
	go func() {
		defer close(g.done)
		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					if !g.closed.Load() {
						g.markChanged()
					}
					return
				}
				if relevant(event) {
					g.markChanged()
				}
			case _, ok := <-w.Errors:
				if !ok {
					if !g.closed.Load() {
						g.markChanged()
					}
					return
				}
				g.markChanged()
			}
		}
	}()
	return g, nil
}

// Changed is monotonic: returning to old content cannot revive a review.
func (g *Guard) Changed() bool { return g.changed.Load() }

func (g *Guard) markChanged() {
	if !g.changed.Swap(true) {
		close(g.change)
	}
}

// Changes closes on the first mutation or observation failure.
func (g *Guard) Changes() <-chan struct{} { return g.change }

// Done closes after the native observation loop has stopped.
func (g *Guard) Done() <-chan struct{} { return g.done }

// Check supplements notifications with content and durable observation checks.
func (g *Guard) Check(ctx context.Context) error {
	if g.Changed() {
		return ErrChanged
	}
	// kqueue may coalesce a create/delete before it can enumerate the new
	// child. Directory metadata detects that lost transient mutation. New or
	// removed ignored files can therefore conservatively invalidate a pass.
	for dir, before := range g.directories {
		st, err := os.Stat(dir)
		if err != nil || statObservation(st) != before {
			g.markChanged()
			return ErrChanged
		}
	}
	m, err := Capture(ctx, g.manifest.Root)
	if err != nil {
		return err
	}
	if g.Changed() || m.ID != g.manifest.ID || m.ObservationID != g.manifest.ObservationID {
		g.markChanged()
		return ErrChanged
	}
	return nil
}

// Close releases all native watches and waits for the consumer to exit.
func (g *Guard) Close() { g.once.Do(func() { g.closed.Store(true); _ = g.watcher.Close(); <-g.done }) }
