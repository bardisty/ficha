// Package paths maps between the directories people work in and the ones
// Claude Code stores transcripts in. It finds the Claude config dir
// (CLAUDE_CONFIG_DIR or ~/.claude), encodes a working path into a project
// directory name the way Claude Code does, and matches a path or a bare
// project name against the known projects. Claude Code shortens names past
// 200 characters with a hash this package can't reproduce; cmd finds those
// projects by the path their transcripts recorded.
//
// A miss or an ambiguous name comes back as an error value that cmd rewords
// with commands to try. ResolveProjectDir's errors for a bad --project-dir
// are the exception: they're worded for a person already, and cmd only
// shortens the path in them on a terminal.
package paths
