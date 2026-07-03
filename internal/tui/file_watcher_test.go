package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type awaitResult struct {
	changed bool
	err     error
}

// startAwait creates a session-file watcher and runs awaitSessionFileChange in
// a goroutine, returning the channel its result will arrive on.
func startAwait(t *testing.T, sessionPath string, done chan struct{}) chan awaitResult {
	t.Helper()

	watcher, err := newSessionFileWatcher(sessionPath)
	if err != nil {
		t.Fatalf("newSessionFileWatcher: %v", err)
	}
	t.Cleanup(func() { watcher.Close() })

	resultCh := make(chan awaitResult, 1)
	go func() {
		changed, err := awaitSessionFileChange(watcher, done, sessionPath)
		resultCh <- awaitResult{changed, err}
	}()
	return resultCh
}

func expectChanged(t *testing.T, resultCh chan awaitResult) {
	t.Helper()
	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("awaitSessionFileChange error: %v", res.err)
		}
		if !res.changed {
			t.Fatal("awaitSessionFileChange returned changed=false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for file change to be detected")
	}
}

func writeSessionFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestAwaitSessionFileChange_Write(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeSessionFile(t, path)

	done := make(chan struct{})
	defer close(done)
	resultCh := startAwait(t, path, done)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteString("{}\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	f.Close()

	expectChanged(t, resultCh)
}

func TestAwaitSessionFileChange_DeleteRecreate(t *testing.T) {
	// A watch on the file itself follows the inode and dies silently when the
	// file is deleted; the dir-based watch must survive delete+recreate.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeSessionFile(t, path)

	done := make(chan struct{})
	defer close(done)
	resultCh := startAwait(t, path, done)

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	writeSessionFile(t, path)

	expectChanged(t, resultCh)
}

func TestAwaitSessionFileChange_AtomicReplace(t *testing.T) {
	// Atomic writes (write temp file, rename onto target) never touch the
	// original inode; the rename must be seen as a change to the target.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeSessionFile(t, path)

	done := make(chan struct{})
	defer close(done)
	resultCh := startAwait(t, path, done)

	tmp := filepath.Join(dir, "session.jsonl.tmp")
	writeSessionFile(t, tmp)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	expectChanged(t, resultCh)
}

func TestAwaitSessionFileChange_IgnoresSiblingFiles(t *testing.T) {
	// The watcher observes the whole parent directory; events for other files
	// in it must not be reported as changes to the session file.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeSessionFile(t, path)

	done := make(chan struct{})
	resultCh := startAwait(t, path, done)

	writeSessionFile(t, filepath.Join(dir, "other-session.jsonl"))

	select {
	case res := <-resultCh:
		t.Fatalf("returned on sibling-file event: %+v", res)
	case <-time.After(300 * time.Millisecond):
	}

	// Shutdown path: closing done unblocks with (false, nil)
	close(done)
	select {
	case res := <-resultCh:
		if res.changed || res.err != nil {
			t.Fatalf("shutdown result = %+v, want changed=false err=nil", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shutdown to unblock the waiter")
	}
}
