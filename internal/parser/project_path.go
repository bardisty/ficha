package parser

// ProjectOriginalPath returns the real directory a project storage directory
// stands for, from sessions-index.json when present, else the transcripts'
// cwd. Returns "" when neither records it.
func ProjectOriginalPath(projectDir string) string {
	if p := getOriginalPathFromIndex(projectDir); p != "" {
		return p
	}
	return originalPathFromTranscripts(projectDir)
}
