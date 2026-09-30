package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// rootArgs validates bare `ficha [session-id]`. The root accepts a positional
// session ID, so cobra's own unknown-command check never runs there and a typo
// like `ficha lst` would otherwise surface as a missing session. A non-hex
// word that is close to a command name, or names a directory, is rejected
// here, before any disk work.
//
// Other non-hex words still go to the session lookup (see rootRun): older
// Claude Code wrote agent-<id>.jsonl transcripts at the project root, and
// those list as sessions whose IDs aren't hex.
func rootArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
		return err
	}
	if len(args) == 0 || looksLikeSessionID(args[0]) {
		return nil
	}
	if len(cmd.SuggestionsFor(args[0])) > 0 || isDir(args[0]) {
		return unknownCommandError(cmd, args[0])
	}
	return nil
}

// rootRun is bare `ficha [session-id]`: show, except that a non-hex word no
// session matched is reported as the mistyped command it most likely is.
func rootRun(cfg *config, cmd *cobra.Command, args []string) error {
	err := runShow(cfg, args, cfg.live)
	var lookupErr *sessionLookupError
	if len(args) == 1 && !looksLikeSessionID(args[0]) && errors.As(err, &lookupErr) {
		return unknownCommandError(cmd, args[0])
	}
	return err
}

// looksLikeSessionID reports whether arg could be a (partial) session ID: hex
// digits and dashes, optionally followed by the .jsonl extension of a pasted
// transcript filename.
func looksLikeSessionID(arg string) bool {
	for _, r := range trimJSONL(arg) {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !isHex && r != '-' {
			return false
		}
	}
	return true
}

// trimJSONL strips a trailing .jsonl, in any case, from a pasted transcript
// filename.
func trimJSONL(arg string) string {
	if len(arg) >= len(".jsonl") && strings.EqualFold(arg[len(arg)-len(".jsonl"):], ".jsonl") {
		return arg[:len(arg)-len(".jsonl")]
	}
	return arg
}

// normalizeSessionID turns what the user typed into what session IDs look
// like on disk: lowercase (UUIDs are case-insensitive, and some tools print
// them in uppercase) with no .jsonl extension.
func normalizeSessionID(arg string) string {
	return strings.ToLower(trimJSONL(arg))
}

func unknownCommandError(cmd *cobra.Command, arg string) error {
	msg := fmt.Sprintf("unknown command %q for %q.", arg, cmd.CommandPath())
	if suggestions := cmd.SuggestionsFor(arg); len(suggestions) > 0 {
		return usageErrorf("%s Did you mean %s?", msg, joinQuoted(suggestions, "or"))
	}
	// A directory is the likeliest non-command word: people expect
	// `ficha <dir>` to analyze that project.
	if isDir(arg) {
		return usageErrorf("%s To analyze that directory, run: ficha -p %s", msg, shellQuote(arg))
	}
	return usageErrorf("%s Run 'ficha --help' to see the commands.", msg)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// shellQuote makes a path in a suggested command safe to paste: unchanged
// when it has no shell metacharacters, single-quoted otherwise.
func shellQuote(s string) string {
	safe := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-~+=:,@%", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// joinQuoted renders ["a"] as `"a"` and ["a", "b", "c"] as `"a", "b" or "c"`.
func joinQuoted(items []string, conj string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = strconv.Quote(s)
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " " + conj + " " + quoted[len(quoted)-1]
}

// noArgs replaces cobra.NoArgs, whose `unknown command "x" for "ficha list"`
// reads as if list had subcommands.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%s takes no arguments, got %q", cmd.CommandPath(), args[0])
	}
	return nil
}

// flagError rewrites pflag's value-parse errors, which quote Go's strconv
// internals, into the shape ficha's own flag validation uses. Other flag
// errors (unknown flag, missing value) already read well and pass through.
// cobra calls it for every flag parse error, so all of them exit 2.
func flagError(_ *cobra.Command, err error) error {
	var invalid *pflag.InvalidValueError
	if !errors.As(err, &invalid) {
		return usageError(err)
	}
	f := invalid.GetFlag()
	var want string
	switch typ := f.Value.Type(); {
	case strings.HasPrefix(typ, "int") || strings.HasPrefix(typ, "uint"):
		want = "must be a whole number"
		if errors.Is(err, strconv.ErrRange) {
			want = "is out of range"
		}
	case typ == "bool":
		want = "must be true or false"
	default:
		return usageError(err)
	}
	return usageErrorf("invalid --%s value %q: %s", f.Name, invalid.GetValue(), want)
}
