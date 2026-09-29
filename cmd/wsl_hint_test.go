package cmd

import (
	"runtime"
	"testing"
)

func TestIsWSLPath(t *testing.T) {
	for _, tc := range []struct {
		dir  string
		want bool
	}{
		{`\\wsl.localhost\Ubuntu\home\you\work\webapp`, true},
		{`\\WSL.LOCALHOST\Ubuntu\home\you`, true},
		{`\\wsl$\Ubuntu\home\you`, true},
		{`\\Wsl$\Debian`, true},
		{`//wsl.localhost/Ubuntu/home/you`, true},
		{`\\wsl.localhost`, false},
		{`\\wsl.localhostile\share`, false},
		{`\\server\share\wsl.localhost`, false},
		{`C:\Users\you\work\webapp`, false},
		{`/home/you/work/webapp`, false},
		{`/mnt/c/Users/you`, false},
		{``, false},
	} {
		if got := isWSLPath(tc.dir); got != tc.want {
			t.Errorf("isWSLPath(%q) = %v, want %v", tc.dir, got, tc.want)
		}
	}
}

// Only the Windows build can be looking at the wrong profile, so only it
// gets the hint, and only when CLAUDE_CONFIG_DIR leaves it on the default.
func TestWSLHintOnlyOnWindows(t *testing.T) {
	const wslDir = `\\wsl.localhost\Ubuntu\home\you`
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	got := wslHint(wslDir)
	if (got != "") != (runtime.GOOS == "windows") {
		t.Errorf("wslHint on %s = %q", runtime.GOOS, got)
	}
	if got := wslHint(`C:\Users\you\work`); got != "" {
		t.Errorf("wslHint for a Windows path = %q, want none", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", `\\wsl.localhost\Ubuntu\home\you\.claude`)
	if got := wslHint(wslDir); got != "" {
		t.Errorf("wslHint with CLAUDE_CONFIG_DIR set = %q, want none", got)
	}
}
