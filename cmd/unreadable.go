package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/render"
)

// sessionReadError explains a session whose transcript ficha couldn't open
// or read, such as one left owned by root after Claude Code ran under sudo,
// naming the file and what to check. Any other error comes back as it was.
func sessionReadError(id string, err error) error {
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		return fmt.Errorf("analyzing session: %w", err)
	}
	e := newDisplayError("can't read session %s: %v.", shortSessionID(id), pathErr.Err)
	if errors.Is(err, fs.ErrPermission) {
		// The folder names the file as well as its full path would, given
		// the session, and is short enough to keep whole on a line.
		e.add(" Check the owner and mode of its transcript in %s.", errPath(filepath.Dir(pathErr.Path)))
	} else {
		e.add(" The transcript is %s.", errPath(pathErr.Path))
	}
	return e
}

// noSessionReadError explains a project none of whose sessions could be
// read, with the first one's reason and where to look. "Failed to parse"
// stays for transcripts ficha read and couldn't make sense of.
func noSessionReadError(err error) error {
	var e *analyzer.NoSessionReadError
	if !errors.As(err, &e) {
		return fmt.Errorf("analyzing sessions: %w", err)
	}
	which := "this project's one session"
	if e.Sessions > 1 {
		which = fmt.Sprintf("any of this project's %s sessions", render.Count(e.Sessions))
	}
	if !e.Read() {
		return fmt.Errorf("%s failed to parse: %w", strings.TrimPrefix(which, "any of "), e.Err)
	}
	d := newDisplayError("can't read %s: %v%s.", which, unwrapPathError(e.Err), othersFailed(e.Others))
	if errors.Is(e.Err, fs.ErrPermission) {
		d.add(" Check the owner and mode of the files in %s.", errPath(filepath.Dir(e.Path)))
	} else {
		d.add(firstTranscript(e.Sessions)+"%s.", errPath(e.Path))
	}
	return d
}

// firstTranscript leads to the path of the first of n transcripts.
func firstTranscript(n int) string {
	if n == 1 {
		return " The transcript is "
	}
	return " The first is "
}

// othersFailed is how many more sessions failed for other reasons, as it
// follows the first's.
func othersFailed(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(", and %s for other reasons", plural(n, "more session"))
}

// skippedProjectWarning is the warning for a project global left out, which
// names it and why, and where to look when the files are there but can't be
// read. A project none of whose transcripts can be read has no recorded
// path to go by, short of the index, and is named by its folder then.
func skippedProjectWarning(cfg *config, err error) string {
	pe := err.(*analyzer.ProjectError)
	var e *analyzer.NoSessionReadError
	if !errors.As(pe.Err, &e) || !e.Read() {
		return fmt.Sprintf("Warning: left out %s: %v\n", warnPath(cfg, projectLabel(pe.Project)), unwrapPathError(pe.Err))
	}
	folder := warnPath(cfg, filepath.Dir(e.Path))
	var which, files string
	if pe.Project.OriginalPath != "" {
		project := warnPath(cfg, pe.Project.OriginalPath)
		which, files = project+"'s one session", "the files in "+folder
		if e.Sessions > 1 {
			which = fmt.Sprintf("any of %s's %s sessions", project, render.Count(e.Sessions))
		}
	} else {
		which, files = "the one session in "+folder, "its files"
		if e.Sessions > 1 {
			which = fmt.Sprintf("any of the %s sessions in %s", render.Count(e.Sessions), folder)
		}
	}
	where := " Check the owner and mode of " + files + "."
	if !errors.Is(e.Err, fs.ErrPermission) {
		where = firstTranscript(e.Sessions) + warnPath(cfg, e.Path) + "."
	}
	return fmt.Sprintf("Warning: can't read %s: %v%s, so it's not in the totals.%s\n",
		which, unwrapPathError(e.Err), othersFailed(e.Others), where)
}

// warnPath is a path in a warning: whole when it wraps, and on a terminal
// shortened to ~/… under home. Piped, it stays as it is for logs and
// scripts, as an error's path does. It goes by stderrWidth, as formatError
// does, so a warning and an error agree on what a terminal is.
func warnPath(cfg *config, p string) string {
	if stderrWidth(cfg.stderr) > 0 {
		p = tildePath(p)
	}
	return render.NoBreak(p)
}
