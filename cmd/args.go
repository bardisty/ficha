package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	if len(args) == 0 || looksLikeSessionID(args[0]) || isTranscriptPath(args[0]) {
		return oneSessionArg(cmd, args)
	}
	if len(cmd.SuggestionsFor(args[0])) > 0 || isDir(args[0]) {
		return unknownCommandError(cmd, args[0])
	}
	return oneSessionArg(cmd, args)
}

// oneSessionArg replaces cobra.MaximumNArgs(1), whose `accepts at most 1
// arg(s)` doesn't say what the one argument is.
func oneSessionArg(cmd *cobra.Command, args []string) error {
	if len(args) <= 1 {
		return nil
	}
	return fmt.Errorf("%s takes one session ID, got %s", cmd.CommandPath(), joinQuoted(args, "and"))
}

// rootRun is bare `ficha [session-id]`: show, except that a non-hex word no
// session matched, and no file has as its name, is reported as the mistyped
// command it most likely is.
func rootRun(cfg *config, cmd *cobra.Command, args []string) error {
	err := runShow(cfg, args, cfg.live)
	var lookupErr *sessionLookupError
	if len(args) == 1 && !looksLikeSessionID(args[0]) && !isTranscriptPath(args[0]) && !isRegularFile(args[0]) && errors.As(err, &lookupErr) {
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
	if err := claudeFolderError(cmd, arg); err != nil {
		return err
	}
	// A directory is the likeliest non-command word: people expect
	// `ficha <dir>` to analyze that project.
	if isDir(arg) {
		return usageErrorf("%s To analyze that directory, run: ficha -p %s", msg, shellQuote(arg))
	}
	return usageErrorf("%s Run 'ficha --help' to see the commands.", msg)
}

// claudeFolderError explains a directory from Claude Code's own config dir
// passed where a session goes, or returns nil for any other argument. A
// project's folder in the projects dir is what --project-dir takes, by its
// name, and a folder named for a session holds that session's agents, beside
// the transcript that's meant.
func claudeFolderError(cmd *cobra.Command, arg string) error {
	if name, ok := projectFolderName(arg); ok {
		todo := map[string]string{
			"watch":     "watch its sessions",
			"breakdown": "break down its latest session",
		}[cmd.Name()]
		if todo == "" {
			todo = "show its latest session"
		}
		return usageErrorf("%q is a Claude project folder, not a session. To %s, run: %s", arg, todo, rewriteArg(cmd, arg, "--project-dir="+name))
	}
	transcript := strings.TrimRight(arg, `/\`) + ".jsonl"
	if id := trimJSONL(filepath.Base(transcript)); id != "" && looksLikeSessionID(id) && isDir(arg) && isRegularFile(transcript) {
		return usageErrorf("%q holds the agents of session %s, not a session. Run: %s", arg, shortSessionID(id), rewriteArg(cmd, arg, transcript))
	}
	return nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
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
	if err := oneSessionArg(cmd, args); err != nil {
		return err
	}
	if len(args) == 0 || looksLikeSessionID(args[0]) || isTranscriptPath(args[0]) || !isDir(args[0]) {
		return nil
	}
	if err := claudeFolderError(cmd, args[0]); err != nil {
		return err
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
	return rewriteArg(cmd, dir, "-p", dir)
}

// rewriteArg is the command line ficha was run with, with arg replaced by
// tokens right after the command's name, or in arg's place for bare ficha,
// and any project flag dropped.
func rewriteArg(cmd *cobra.Command, arg string, tokens ...string) string {
	line, ok := commandLine(cmd.Context())
	if !ok {
		out := []string{cmd.CommandPath()}
		for _, t := range tokens {
			out = append(out, shellQuote(t))
		}
		return strings.Join(out, " ")
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
				out = append(append(out, a), tokens...)
				continue
			}
		}
		if !moved && a == arg {
			moved = true
			// Bare ficha has no command name on the line to follow.
			if !cmd.HasParent() {
				out = append(out, tokens...)
			}
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
// takes a value, or else the first, and nil when cmd lacks any of them.
func lineFlag(cmd *cobra.Command, token string) (f *pflag.Flag, takesNext bool) {
	// A command that hasn't run has its parents' persistent flags apart.
	lookup := func(name string) *pflag.Flag {
		if f := cmd.Flags().Lookup(name); f != nil {
			return f
		}
		return cmd.InheritedFlags().Lookup(name)
	}
	// pflag panics on a shorthand longer than a byte, and none is.
	shorthand := func(s string) *pflag.Flag {
		if len(s) != 1 {
			return nil
		}
		if f := cmd.Flags().ShorthandLookup(s); f != nil {
			return f
		}
		return cmd.InheritedFlags().ShorthandLookup(s)
	}
	switch {
	case strings.HasPrefix(token, "--") && len(token) > 2:
		name, _, hasValue := strings.Cut(token[2:], "=")
		f = lookup(name)
		return f, f != nil && !hasValue && f.NoOptDefVal == ""
	case strings.HasPrefix(token, "-") && len(token) > 1:
		// Shorthands combine, as in -vf json: the first one that takes a
		// value takes the rest of the token, or the next token.
		var first *pflag.Flag
		for j, r := range token[1:] {
			sf := shorthand(string(r))
			if sf == nil {
				return nil, false
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
// flag that another command takes gets pointed at that command. Other flag
// errors (missing value) already read well and pass through. cobra calls it
// for every flag parse error, so all of them exit 2.
func flagError(cmd *cobra.Command, err error) error {
	var notExist *pflag.NotExistError
	if errors.As(err, &notExist) {
		if hint := commandFlagHint(cmd, notExist); hint != "" {
			if !cmd.HasParent() {
				return usageErrorf("%v. %s", err, hint)
			}
			return usageError(errors.New(hint))
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

// commandFlagHint names the commands that take the unknown flag cmd
// rejected, as in `ficha list --top 5`, and suggests the line with one of
// them in cmd's place. Bare ficha is show, but with only the project flags.
// It's "" when no other command takes the flag, and for commands that
// aren't reports: help and its topics, version, completion and those below
// it. Swapping a report in for one of those makes a line that fails.
func commandFlagHint(cmd *cobra.Command, e *pflag.NotExistError) string {
	root := cmd.Root()
	if cmd != root && (cmd.Parent() != root || !cmd.IsAvailableCommand() || cmd.Name() == "version" || cmd.Name() == "completion") {
		return ""
	}
	// pflag panics on a shorthand longer than a byte, as -é's is, and no
	// command has one.
	if e.GetSpecifiedShortnames() != "" && len(e.GetSpecifiedName()) != 1 {
		return ""
	}
	// Owners that analyze one project, as bare ficha does, come first: they
	// make the better suggestion for `ficha --since 7d`.
	var owners, others []string
	var flag string
	for _, c := range root.Commands() {
		if c == cmd || !c.IsAvailableCommand() {
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
	isProjectFlag := flag == "-p" || flag == "--project" || flag == "--project-dir"
	switch {
	case cmd == root:
		return withSuggestedLine(root, fmt.Sprintf("%s is a %s flag", flag, joinWords(owners, "and")), owners)
	case isProjectFlag && cmd.Name() == "global":
		// Of the commands that take -p, summary is the one global narrows to.
		return withSuggestedLine(root, fmt.Sprintf("%s covers every project and doesn't take %s. summary covers one project", cmd.CommandPath(), flag), []string{"summary"})
	case isProjectFlag && cmd.Name() == "statusline":
		return fmt.Sprintf("%s doesn't take %s: it reads the session from the JSON Claude Code writes on stdin.", cmd.CommandPath(), flag)
	}
	return withSuggestedLine(root, fmt.Sprintf("%s doesn't take %s. %s is a %s flag", cmd.CommandPath(), flag, flag, joinWords(owners, "and")), owners)
}

// withSuggestedLine ends hint with the command line ficha was run with,
// its command swapped for the first of owners that takes the rest of it,
// unless it's one of owners already. It ends it with a period instead when
// the line isn't known, or when no owner takes the rest of the line: a
// suggestion that fails in turn sends the reader in a circle.
func withSuggestedLine(root *cobra.Command, hint string, owners []string) string {
	args, ok := commandLine(root.Context())
	if !ok {
		return hint + "."
	}
	// With the flag first, as in `ficha --details summary`, cobra takes the
	// command name for the unknown flag's value and never reaches it. A name
	// that owns the flag moves to the front; any other gives way to one
	// that does.
	rest := args
	for i, a := range args {
		c := subcommandNamed(root, a)
		if c == nil || !c.IsAvailableCommand() {
			continue
		}
		if slices.Contains(owners, c.Name()) {
			owners = []string{c.Name()}
		}
		rest = append(slices.Clone(args[:i]), args[i+1:]...)
		break
	}
	i := slices.IndexFunc(owners, func(name string) bool {
		return takesLine(subcommandNamed(root, name), rest)
	})
	if i < 0 {
		return hint + "."
	}
	command := owners[i]
	quoted := make([]string, len(rest))
	for i, a := range rest {
		quoted[i] = shellQuote(a)
	}
	return fmt.Sprintf("%s: %s %s %s", hint, root.Name(), command, strings.Join(quoted, " "))
}

// takesLine reports whether cmd takes every flag in args, and the
// arguments left once they're taken out, and whether it can print the
// format they ask for: the live views and statusline print only a table.
func takesLine(cmd *cobra.Command, args []string) bool {
	var positional []string
	format, live := "", false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		f, takesNext := lineFlag(cmd, a)
		if f == nil {
			return false
		}
		switch {
		case f.Name == "live":
			live = true
		case f.Name == "format" && takesNext && i+1 < len(args):
			format = args[i+1]
		case f.Name == "format" && strings.HasPrefix(a, "--"):
			_, format, _ = strings.Cut(a, "=")
		case f.Name == "format":
			_, format, _ = strings.Cut(a, f.Shorthand)
		}
		if takesNext {
			if i+1 == len(args) {
				return false
			}
			i++
		}
	}
	tableOnly := live || cmd.Name() == "watch" || cmd.Name() == "breakdown" || cmd.Name() == "statusline"
	if tableOnly && format != "" && !strings.EqualFold(format, "table") {
		return false
	}
	return cmd.Args == nil || cmd.Args(cmd, positional) == nil
}

// joinWords renders ["a"] as "a", ["a", "b"] as "a and b" and
// ["a", "b", "c"] as "a, b and c", with conj in place of "and".
func joinWords(words []string, conj string) string {
	if len(words) == 1 {
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " " + conj + " " + words[len(words)-1]
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
