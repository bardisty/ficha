package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
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
	if len(args) == 0 || looksLikeSessionID(args[0]) || isTranscriptPath(args[0]) {
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
	if len(args) == 1 && !looksLikeSessionID(args[0]) && !isTranscriptPath(args[0]) && errors.As(err, &lookupErr) {
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

// sessionArgs validates `ficha show|watch|breakdown [session-id]`. People
// pass a project directory there before they find -p, and as a session ID it
// would end in an error about a different directory. As in rootArgs, a
// transcript path and a hex word stay a session lookup, so a directory named
// like an ID is still read as one.
func sessionArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
		return err
	}
	if len(args) == 0 || looksLikeSessionID(args[0]) || isTranscriptPath(args[0]) || !isDir(args[0]) {
		return nil
	}
	todo := map[string]string{
		"show":      "show that directory's latest session",
		"watch":     "watch that directory's sessions",
		"breakdown": "break down that directory's sessions",
	}[cmd.Name()]
	return fmt.Errorf("%q is a directory, not a session. To %s, run: %s", args[0], todo, projectCommand(cmd, args[0]))
}

// noArgs replaces cobra.NoArgs, whose `unknown command "x" for "ficha list"`
// reads as if list had subcommands. A command that takes -p points a
// directory at it.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if cmd.Flags().Lookup("project") != nil && isDir(args[0]) {
		return fmt.Errorf("%s takes no arguments. For that directory's sessions, run: %s", cmd.CommandPath(), projectCommand(cmd, args[0]))
	}
	return fmt.Errorf("%s takes no arguments, got %q", cmd.CommandPath(), args[0])
}

// projectCommand is the command line ficha was run with, with dir moved
// from an argument to -p right after the command's name. A project flag
// already on the line is dropped, since -p replaces it.
func projectCommand(cmd *cobra.Command, dir string) string {
	line, ok := commandLine(cmd.Context())
	if !ok {
		return cmd.CommandPath() + " -p " + shellQuote(dir)
	}
	out := []string{cmd.Root().Name()}
	named, moved, afterDashes := false, false, false
	for i := 0; i < len(line); i++ {
		a := line[i]
		if !afterDashes {
			if a == "--" {
				afterDashes = true
				out = append(out, a)
				continue
			}
			if f, takesNext := lineFlag(cmd, a); f != nil {
				n := 1
				if takesNext && i+1 < len(line) {
					n = 2
				}
				switch {
				case f.Name != "project" && f.Name != "project-dir":
					out = append(out, line[i:i+n]...)
				case !strings.HasPrefix(a, "--"):
					// In a bundle such as -vp, the flags before p stay.
					if before, _, _ := strings.Cut(a, "p"); len(before) > 1 {
						out = append(out, before)
					}
				}
				i += n - 1
				continue
			}
			if !named && (a == cmd.Name() || slices.Contains(cmd.Aliases, a)) {
				named = true
				out = append(out, a, "-p", dir)
				continue
			}
		}
		if !moved && a == dir {
			moved = true
			continue
		}
		out = append(out, a)
	}
	if out[len(out)-1] == "--" {
		out = out[:len(out)-1]
	}
	for i, a := range out {
		out[i] = shellQuote(a)
	}
	return strings.Join(out, " ")
}

// lineFlag is the flag a command line token sets, if any, and whether its
// value is the next token. For a bundle of shorthands it's the one that
// takes a value, or else the first.
func lineFlag(cmd *cobra.Command, token string) (f *pflag.Flag, takesNext bool) {
	switch {
	case strings.HasPrefix(token, "--") && len(token) > 2:
		name, _, hasValue := strings.Cut(token[2:], "=")
		f = cmd.Flags().Lookup(name)
		return f, f != nil && !hasValue && f.NoOptDefVal == ""
	case strings.HasPrefix(token, "-") && len(token) > 1:
		// Shorthands combine, as in -vf json: the first one that takes a
		// value takes the rest of the token, or the next token.
		var first *pflag.Flag
		for j, r := range token[1:] {
			sf := cmd.Flags().ShorthandLookup(string(r))
			if sf == nil {
				return first, false
			}
			if first == nil {
				first = sf
			}
			if sf.NoOptDefVal == "" {
				return sf, j == len(token)-2
			}
		}
		return first, false
	}
	return nil, false
}

// flagError rewrites pflag's value-parse errors, which quote Go's strconv
// internals, into the shape ficha's own flag validation uses. An unknown
// flag on bare `ficha` that a command takes gets pointed at that command.
// Other flag errors (missing value) already read well and pass through.
// cobra calls it for every flag parse error, so all of them exit 2.
func flagError(cmd *cobra.Command, err error) error {
	var notExist *pflag.NotExistError
	if errors.As(err, &notExist) && !cmd.HasParent() {
		if hint := commandFlagHint(cmd, notExist); hint != "" {
			return usageErrorf("%v. %s", err, hint)
		}
	}
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

// commandFlagHint names the commands that take the unknown flag bare
// `ficha` rejected, as in `ficha -f csv --messages`. Bare ficha is show,
// but with only the project flags.
func commandFlagHint(root *cobra.Command, e *pflag.NotExistError) string {
	// Owners that analyze one project, as bare ficha does, come first: they
	// make the better suggestion for `ficha --since 7d`.
	var owners, others []string
	var flag string
	for _, c := range root.Commands() {
		if !c.IsAvailableCommand() {
			continue
		}
		// For a shorthand, the name is the one letter pflag didn't know.
		f := c.Flags().Lookup(e.GetSpecifiedName())
		if e.GetSpecifiedShortnames() != "" {
			f = c.Flags().ShorthandLookup(e.GetSpecifiedName())
		}
		if f == nil || f.Hidden {
			continue
		}
		if c.Flags().Lookup("project") != nil {
			owners = append(owners, c.Name())
		} else {
			others = append(others, c.Name())
		}
		flag = "--" + f.Name
		if e.GetSpecifiedShortnames() != "" {
			flag = "-" + f.Shorthand
		}
	}
	owners = append(owners, others...)
	if len(owners) == 0 {
		return ""
	}
	hint := fmt.Sprintf("%s is a %s flag", flag, strings.Join(owners, " and "))
	args, ok := commandLine(root.Context())
	if !ok {
		return hint + "."
	}
	// With the flag first, as in `ficha --details summary`, cobra takes the
	// command name for the unknown flag's value and never reaches it. A name
	// that owns the flag moves to the front; any other leaves the fix to the
	// reader.
	command := owners[0]
	var rest []string
	for i, a := range args {
		c := subcommandNamed(root, a)
		if c == nil || !c.IsAvailableCommand() {
			continue
		}
		if !slices.Contains(owners, c.Name()) {
			return hint + "."
		}
		command = c.Name()
		rest = append(slices.Clone(args[:i]), args[i+1:]...)
		break
	}
	if rest == nil {
		rest = args
	}
	quoted := make([]string, len(rest))
	for i, a := range rest {
		quoted[i] = shellQuote(a)
	}
	return fmt.Sprintf("%s: %s %s %s", hint, root.Name(), command, strings.Join(quoted, " "))
}

type commandLineKey struct{}

// withCommandLine carries the arguments ficha was run with, which cobra
// doesn't pass to a flag error function, so a hint can repeat them.
func withCommandLine(ctx context.Context, args []string) context.Context {
	return context.WithValue(ctx, commandLineKey{}, args)
}

func commandLine(ctx context.Context) ([]string, bool) {
	if ctx == nil {
		return nil, false
	}
	args, ok := ctx.Value(commandLineKey{}).([]string)
	return args, ok
}
