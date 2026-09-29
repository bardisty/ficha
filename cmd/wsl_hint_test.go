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

// Only the Windows build on its default config is looking at the wrong
// profile. wslHint and waitingProject both go by windowsDefaultInWSL, so the
// hint shows exactly where watch and breakdown decline to wait.
func TestWindowsDefaultInWSL(t *testing.T) {
	const wslDir = `\\wsl.localhost\Ubuntu\home\you`
	onWindows := runtime.GOOS == "windows"
	for _, tc := range []struct {
		name, dir, configDir string
		want                 bool
	}{
		{"WSL dir, default config", wslDir, "", onWindows},
		{"Windows dir, default config", `C:\Users\you\work`, "", false},
		{"WSL dir, CLAUDE_CONFIG_DIR into WSL", wslDir, `\\wsl.localhost\Ubuntu\home\you\.claude`, false},
		{"WSL dir, CLAUDE_CONFIG_DIR on Windows", wslDir, `C:\Users\you\.claude-work`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_CONFIG_DIR", tc.configDir)
			if got := windowsDefaultInWSL(tc.dir); got != tc.want {
				t.Errorf("windowsDefaultInWSL(%q) on %s = %v, want %v", tc.dir, runtime.GOOS, got, tc.want)
			}
			if got := wslHint(tc.dir); (got != "") != tc.want {
				t.Errorf("wslHint(%q) on %s = %q, want a hint: %v", tc.dir, runtime.GOOS, got, tc.want)
			}
		})
	}
}
