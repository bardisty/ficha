package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func sessionIDOf(t *testing.T, stdout string) string {
	t.Helper()
	var s struct {
		SessionID string `json:"session_id"`
		Project   string `json:"project"`
	}
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatalf("not json: %v\n%s", err, stdout)
	}
	return s.SessionID
}

func TestTranscriptPathArgument(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)
	beta := filepath.Join(proj, e2eBetaID+".jsonl")

	want, _, err := executeCLISplit(t, "show", projFlag, e2eBetaID, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"show", beta, "-f", "json"},
		{beta, "-f", "json"},
		// The path names its own project; -p pointing elsewhere doesn't apply.
		{"show", "-p", t.TempDir(), beta, "-f", "json"},
	} {
		got, stderr, err := executeCLISplit(t, args...)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if got != want {
			t.Errorf("%v differs from the ID lookup:\n got: %s\nwant: %s", args, got, want)
		}
		if stderr != "" {
			t.Errorf("%v wrote to stderr: %q", args, stderr)
		}
	}

	// Relative, and a bare file name that exists here.
	t.Chdir(proj)
	for _, arg := range []string{e2eBetaID + ".jsonl", "." + string(filepath.Separator) + e2eBetaID + ".jsonl"} {
		out, _, err := executeCLISplit(t, "show", arg, "-f", "json")
		if err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		if id := sessionIDOf(t, out); id != e2eBetaID {
			t.Errorf("%s shows %s", arg, id)
		}
	}
}

func TestTranscriptPathErrors(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)
	beta := filepath.Join(proj, e2eBetaID+".jsonl")
	agent := filepath.Join(proj, e2eBetaID, "subagents", "agent-g1.jsonl")
	wfAgent := filepath.Join(proj, e2eBetaID, "subagents", "workflows", "wf_e2e-run1", "agent-w1.jsonl")
	missing := filepath.Join(t.TempDir(), "gone.jsonl")

	agentErr := func(file string) string {
		return file + " is an agent's transcript, part of session bbbbbbbb. Run: ficha show " + shellQuote(beta)
	}
	tests := []struct {
		name     string
		args     []string
		wantErr  string
		wantCode int
	}{
		{"agent transcript", []string{"show", agent}, agentErr("agent-g1.jsonl"), exitUsage},
		{"workflow agent transcript", []string{"show", wfAgent}, agentErr("agent-w1.jsonl"), exitUsage},
		{"missing path", []string{"show", missing}, "can't read transcript " + missing + ": ", 1},
		// Not "unknown command": it's a path.
		{"missing path, bare ficha", []string{missing}, "can't read transcript " + missing + ": ", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executeCLISplit(t, tt.args...)
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Fatalf("\n got: %v\nwant: %s", err, tt.wantErr)
			}
			if got := exitCode(err); got != tt.wantCode {
				t.Errorf("exit %d, want %d", got, tt.wantCode)
			}
		})
	}
}

// A full session ID can't be ambiguous, so one found in another project is
// shown, with the project named on stderr and stdout left as clean json.
func TestFullSessionIDFromAnotherProject(t *testing.T) {
	setupE2EFixture(t)
	note := "Note: session cccccccc is in " + e2eOtherDir + ".\n"
	for _, args := range [][]string{
		{"show", projFlag, e2eGammaID, "-f", "json"},
		{"show", projFlag, strings.ToUpper(e2eGammaID) + ".jsonl", "-f", "json"},
		// This directory has no project at all.
		{"show", "-p", t.TempDir(), e2eGammaID, "-f", "json"},
	} {
		stdout, stderr, err := executeCLISplit(t, args...)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if id := sessionIDOf(t, stdout); id != e2eGammaID {
			t.Errorf("%v shows %s", args, id)
		}
		if stderr != note {
			t.Errorf("%v stderr %q, want %q", args, stderr, note)
		}
	}
}
