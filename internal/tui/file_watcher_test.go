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

func TestSubagentTreeSignature(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess-sig"

	// No subagents dir at all
	if sig := subagentTreeSignature(tmpDir, sessionID); sig != "" {
		t.Errorf("expected empty signature for missing dirs, got %q", sig)
	}

	// Regular agent file appears
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(subagentsDir, "agent-a1.jsonl")
	if err := os.WriteFile(agentPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	sig1 := subagentTreeSignature(tmpDir, sessionID)
	if sig1 == "" {
		t.Fatal("expected non-empty signature after agent file created")
	}

	// Stable when nothing changes
	if sig := subagentTreeSignature(tmpDir, sessionID); sig != sig1 {
		t.Error("signature changed with no filesystem changes")
	}

	// Append to the agent file (size change; mtime may have coarse
	// granularity on some filesystems, size alone must flip the signature)
	if err := os.WriteFile(agentPath, []byte("{}\n{}"), 0644); err != nil {
		t.Fatal(err)
	}
	sig2 := subagentTreeSignature(tmpDir, sessionID)
	if sig2 == sig1 {
		t.Error("signature unchanged after file append")
	}

	// New workflow run dir with an agent file
	runDir := filepath.Join(subagentsDir, "workflows", "wf_run-1")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "agent-w1.jsonl"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	sig3 := subagentTreeSignature(tmpDir, sessionID)
	if sig3 == sig2 {
		t.Error("signature unchanged after workflow agent file created")
	}

	// Workflow metadata write (status flip without transcript writes)
	wfDir := filepath.Join(tmpDir, sessionID, "workflows")
	if err := os.MkdirAll(wfDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, "wf_run-1.json"), []byte(`{"status":"completed"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if sig := subagentTreeSignature(tmpDir, sessionID); sig == sig3 {
		t.Error("signature unchanged after workflow metadata write")
	}
}
