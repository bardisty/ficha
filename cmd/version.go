package cmd

import (
	"fmt"
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

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "ficha %s\n", version())
			return nil
		},
	}
}
