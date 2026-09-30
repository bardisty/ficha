package cmd

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func TestFormatVersionLine(t *testing.T) {
	const rev = "01ff73c02a6c1f3b9c7e4d2a8b5f60e1d9c3a7b4"
	vcs := func(modified string) *debug.BuildInfo {
		info := &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "GOOS", Value: "linux"},
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: rev},
		}}
		if modified != "" {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs.modified", Value: modified})
		}
		return info
	}
	tail := runtime.Version() + ", " + runtime.GOOS + "/" + runtime.GOARCH + ")"
	tests := []struct {
		name    string
		version string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"release build from a clean tree", "0.54.0", vcs("false"), true, "ficha 0.54.0 (01ff73c, " + tail},
		{"release build from a dirty tree", "0.54.0", vcs("true"), true, "ficha 0.54.0 (01ff73c+dirty, " + tail},
		{"pseudo-version already names the commit", "0.54.1-0.20260929173200-01ff73c02a6c", vcs("false"), true, "ficha 0.54.1-0.20260929173200-01ff73c02a6c (" + tail},
		{"pseudo-version already marks it dirty", "0.54.1-0.20260929173200-01ff73c02a6c+dirty", vcs("true"), true, "ficha 0.54.1-0.20260929173200-01ff73c02a6c+dirty (" + tail},
		{"plain build of a tag from a dirty tree", "0.55.0+dirty", vcs("true"), true, "ficha 0.55.0+dirty (01ff73c, " + tail},
		{"go install has no vcs settings", "0.54.0", &debug.BuildInfo{}, true, "ficha 0.54.0 (" + tail},
		{"no build info", "dev", nil, false, "ficha dev (" + tail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatVersionLine(tt.version, tt.info, tt.ok); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Scripts that ran `ficha version | awk '{print $2}'` against the bare line
// must still get the number.
func TestVersionLineSecondFieldIsVersion(t *testing.T) {
	out, err := executeCLI(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(out)
	if len(fields) < 2 || fields[0] != "ficha" || fields[1] != version() {
		t.Errorf("version output %q: want \"ficha %s ...\"", out, version())
	}
}
