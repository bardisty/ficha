package cmd

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Version is set via ldflags at build time
var Version = "dev"

// version is the version ficha reports. `go install module@vX.Y.Z` skips the
// Makefile's ldflags, so without the build-info fallback those binaries would
// all claim to be "dev".
func version() string {
	info, ok := debug.ReadBuildInfo()
	return resolveVersion(Version, info, ok)
}

func resolveVersion(ldflagsVersion string, info *debug.BuildInfo, ok bool) string {
	if ldflagsVersion != "dev" || !ok || info == nil {
		return ldflagsVersion
	}
	// "(devel)" is what Go stamps when it has no version to give (tests,
	// builds outside a module or VCS checkout).
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return ldflagsVersion
	}
	// Tags are "vX.Y.Z" but the VERSION file, and so the ldflags value, has
	// no "v"; print both the same way.
	return strings.TrimPrefix(v, "v")
}

// versionLine is what `ficha version` and `--version` print: the
// version, then the commit, Go version and platform a bug report needs.
// version() stays the bare number, which the unknown-model warning prints.
func versionLine() string {
	info, ok := debug.ReadBuildInfo()
	return formatVersionLine(version(), info, ok)
}

// formatVersionLine keeps the "ficha X.Y.Z" prefix so scripts that split on
// the space still get the number. The commit is only as good as the build
// info: `go install module@vX` builds carry no vcs settings, so it's left out
// rather than guessed.
func formatVersionLine(v string, info *debug.BuildInfo, ok bool) string {
	var parts []string
	if ok && info != nil {
		if c := commit(v, info); c != "" {
			parts = append(parts, c)
		}
	}
	// The toolchain and target are compiled into the binary, so these are
	// right even when there's no build info.
	parts = append(parts, runtime.Version(), runtime.GOOS+"/"+runtime.GOARCH)
	return fmt.Sprintf("ficha %s (%s)", v, strings.Join(parts, ", "))
}

// commit returns the short revision, marked "+dirty" when the tree had
// uncommitted changes. It returns "" when the version already names the
// revision: a plain `go build` in a checkout reports a pseudo-version ending
// in the commit. Go appends its own "+dirty" to that version, and to a tag's,
// so the mark is left off when the version has it.
func commit(v string, info *debug.BuildInfo) string {
	var rev string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) >= 12 && strings.Contains(v, rev[:12]) {
		return ""
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if modified && !strings.HasSuffix(v, "+dirty") {
		rev += "+dirty"
	}
	return rev
}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "version",
		Short:             "Print version information",
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), versionLine())
			return nil
		},
	}
	takeNoGlobalFlags(cmd)
	return cmd
}
