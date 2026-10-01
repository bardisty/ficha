package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The warnings ficha prints outside a report wrap to the terminal like its
// notes do, hung under the text after "Warning: ", and stay one line when
// stderr is redirected.
func TestFlagWarningsWrap(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		oneLine string
		wrapped string
	}{
		{
			name:    "--messages on a table",
			args:    []string{"show", projFlag, e2eAlphaID, "--messages"},
			oneLine: "Warning: --messages has no effect on table output (use -f json or -f csv)\n",
			wrapped: "Warning: --messages has no effect on\n" +
				"         table output (use -f json or -f\n" +
				"         csv)\n",
		},
		{
			name:    "--no-follow outside a live view",
			args:    []string{"show", projFlag, e2eAlphaID, "--no-follow", "-f", "json"},
			oneLine: "Warning: --no-follow has no effect outside live/watch/breakdown mode\n",
			wrapped: "Warning: --no-follow has no effect\n" +
				"         outside live/watch/breakdown\n" +
				"         mode\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupE2EFixture(t)
			_, stderr, err := executeCLISplit(t, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if stderr != tt.oneLine {
				t.Errorf("redirected:\n got: %q\nwant: %q", stderr, tt.oneLine)
			}

			withStderrWidth(t, 40)
			_, stderr, err = executeCLISplit(t, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if stderr != tt.wrapped {
				t.Errorf("at 40 columns:\n got: %q\nwant: %q", stderr, tt.wrapped)
			}
		})
	}
}

// The sessions-index warning names the file it couldn't read. With a space
// in the config directory, the path must come through whole at any width,
// and exactly as the error spelled it when stderr is redirected.
func TestSessionsIndexWarningKeepsItsPathWhole(t *testing.T) {
	root := setupE2EFixture(t)
	spaced := filepath.Join(t.TempDir(), "Claude  Config")
	if err := os.Rename(root, spaced); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", spaced)
	// A directory where the index should be: it can be found but not read
	// as a file, on every OS and for every user, root included
	index := filepath.Join(spaced, "projects", e2eProjDir, "sessions-index.json")
	if err := os.Mkdir(index, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"show", projFlag, e2eAlphaID, "-f", "json"}

	_, stderr, err := executeCLISplit(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	const label = "Warning: failed to parse sessions-index.json: "
	if !strings.HasPrefix(stderr, label) || strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, " "+index+": ") {
		t.Errorf("redirected, want one line naming %q, got %q", index, stderr)
	}
	if strings.Contains(stderr, "\x00") {
		t.Errorf("the path's mark reached stderr: %q", stderr)
	}
	oneLine := stderr

	for _, width := range []int{40, 60, 80} {
		withStderrWidth(t, width)
		_, stderr, err = executeCLISplit(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
		if len(lines) < 2 {
			t.Fatalf("at %d the warning didn't wrap: %q", width, stderr)
		}
		// The path is longer than any of these widths, so it starts at the
		// left edge on a line of its own. Every other line hangs.
		whole := false
		for i, line := range lines {
			switch {
			case line == index+":":
				whole = true
			case i > 0 && !strings.HasPrefix(line, "         "):
				t.Errorf("at %d line %d isn't hung under the warning's text: %q", width, i+1, line)
			}
		}
		if !whole {
			t.Errorf("at %d the path isn't whole on a line of its own:\n%s", width, stderr)
		}
		// Nothing but the line breaks and their indents may differ
		got := strings.ReplaceAll(stderr, "\n         ", " ")
		got = strings.ReplaceAll(strings.TrimSuffix(got, "\n"), "\n", " ") + "\n"
		if got != oneLine {
			t.Errorf("at %d the text changed:\n got: %q\nwant: %q", width, got, oneLine)
		}
	}
}

// -v lists each damaged transcript's path on a line of its own. That line
// has nothing to hang from, so a path with a space would wrap to the left
// margin of its block and read as two.
func TestSkippedFilePathStaysWholeWhenWrapped(t *testing.T) {
	root := setupE2EFixture(t)
	spaced := filepath.Join(t.TempDir(), "Claude Config")
	if err := os.Rename(root, spaced); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", spaced)
	transcript := filepath.Join(spaced, "projects", e2eProjDir, e2eAlphaID+".jsonl")
	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not json\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	withStderrWidth(t, 40)
	_, stderr, err := executeCLISplit(t, "show", projFlag, e2eAlphaID, "-v", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "\n    "+transcript+"\n") {
		t.Errorf("want the transcript's path whole on its own line, got:\n%s", stderr)
	}
}
