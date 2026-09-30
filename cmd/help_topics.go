package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// helpTopicArgs rejects `ficha help <word>` when no command or help topic
// under that path has the name. Without it, cobra's help finds the root for
// any word, because the root takes a session ID, and prints the root help.
func helpTopicArgs(cmd *cobra.Command, args []string) error {
	parent := cmd.Root()
	for _, arg := range args {
		next := subcommandNamed(parent, arg)
		if next == nil {
			return unknownHelpTopicError(parent, arg)
		}
		parent = next
	}
	return nil
}

func subcommandNamed(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name || c.HasAlias(name) {
			return c
		}
	}
	return nil
}

func unknownHelpTopicError(parent *cobra.Command, arg string) error {
	msg := fmt.Sprintf("unknown help topic %q for %q.", arg, parent.CommandPath())
	if suggestions := helpSuggestions(parent, arg); len(suggestions) > 0 {
		return usageErrorf("%s Did you mean %s?", msg, joinQuoted(suggestions, "or"))
	}
	if !parent.HasSubCommands() {
		return usageErrorf("%s It has no subtopics. Run '%s --help'.", msg, parent.CommandPath())
	}
	return usageErrorf("%s Run '%s --help' to see the commands and help topics.", msg, parent.CommandPath())
}

// helpSuggestions is cobra's SuggestionsFor with help topics included:
// cobra skips commands that have no Run, and topics like `output` and
// `environment` have none.
func helpSuggestions(parent *cobra.Command, typed string) []string {
	var out []string
	for _, c := range parent.Commands() {
		if c.Hidden || !(c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand()) {
			continue
		}
		name := strings.ToLower(c.Name())
		t := strings.ToLower(typed)
		if strings.HasPrefix(name, t) || editDistance(name, t) <= parent.Root().SuggestionsMinimumDistance {
			out = append(out, c.Name())
		}
	}
	return out
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// takeNoGlobalFlags keeps the global flags out of cmd's help and completion.
// version and the completion scripts print fixed text, so no global flag
// changes them, and noGlobalFlags rejects any that are given. cmd must
// already be in the tree, under a root that has its global flags.
//
// cobra has no way to leave inherited flags out of one command, so each
// global flag gets a hidden local copy of the same name. The copy shadows the
// global one, which drops it from InheritedFlags, and so from the help's
// Global Flags block and from completion, which skips hidden flags. The copy
// still parses the global flag's value, so noGlobalFlags can name it.
func takeNoGlobalFlags(cmd *cobra.Command) {
	cmd.Root().PersistentFlags().VisitAll(func(f *pflag.Flag) {
		shadow := *f
		shadow.Hidden = true
		cmd.Flags().AddFlag(&shadow)
	})
	cmd.Annotations = map[string]string{noGlobalFlagsAnnotation: "true"}
}

const noGlobalFlagsAnnotation = "ficha/no-global-flags"

// noGlobalFlags returns a usage error naming the first global flag given to a
// command that takes none.
func noGlobalFlags(cmd *cobra.Command) error {
	if cmd.Annotations[noGlobalFlagsAnnotation] == "" {
		return nil
	}
	var given string
	cmd.Root().PersistentFlags().VisitAll(func(g *pflag.Flag) {
		if f := cmd.Flags().Lookup(g.Name); f != nil && f.Changed && given == "" {
			given = "--" + f.Name
		}
	})
	if given != "" {
		return usageErrorf("%s doesn't take %s", cmd.CommandPath(), given)
	}
	return nil
}
