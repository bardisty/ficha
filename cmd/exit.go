package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// exitError carries an exit status other than 1. An empty message means the
// status is the whole report and Execute prints nothing.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

// exitUsage is the status for a command line ficha can't act on: an unknown
// command or flag, a bad value, or flags that don't go together. It follows
// grep, curl and Go's flag package, so a script can tell "called wrong" from
// "nothing to report".
const exitUsage = 2

// usageError marks err as the caller's mistake, so ficha exits 2.
func usageError(err error) error {
	return &exitError{code: exitUsage, err: err}
}

func usageErrorf(format string, a ...any) error {
	return usageError(fmt.Errorf(format, a...))
}

// usageArgs makes every argument validator in the tree, cobra's own included,
// fail with a usage error: an Args check only ever rejects how the command
// was called.
func usageArgs(cmd *cobra.Command) {
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(c *cobra.Command, args []string) error {
			if err := validate(c, args); err != nil {
				return usageError(err)
			}
			return nil
		}
	}
	for _, sub := range cmd.Commands() {
		usageArgs(sub)
	}
}

// exitCode is the status Execute exits with for err: 0 for success, the
// carried code for an exitError, and 1 for everything else.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *exitError
	if errors.As(err, &e) {
		return e.code
	}
	return 1
}
