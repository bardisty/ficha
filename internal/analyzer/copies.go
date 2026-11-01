package analyzer

import (
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
)

// FindCopies returns the replies in a session's transcript that list and
// summary count under another session in its project, priced, and which
// session that is. It returns nil when there are none.
//
// Only the sessions sharing a reply with this one matter to who keeps it,
// and a fork copies its original from the start, so those are the sessions
// whose first reply is this one's. Comparing first replies reads a few KB of
// each transcript where searching them whole would read the project, and
// only that family gets parsed. Agent transcripts are never copied.
//
// checked is false only while the session has no reply with an ID yet, so
// watch knows to look again. A transcript outside the projects directory
// has no neighbors to compare with, and gets nil, checked.
func FindCopies(sessionPath, sessionID string) (copies *models.CopiedReplies, checked bool) {
	sessionPath, err := filepath.Abs(sessionPath)
	if err != nil {
		return nil, true
	}
	projectDir := filepath.Dir(sessionPath)
	projectsDir, err := paths.GetProjectsDir()
	if err == nil {
		projectsDir, err = filepath.Abs(projectsDir)
	}
	if err != nil || filepath.Dir(projectDir) != projectsDir {
		return nil, true
	}
	firstID, err := parser.FirstReplyID(sessionPath)
	if err != nil {
		return nil, true
	}
	if firstID == "" {
		return nil, false
	}

	entries := projectSessions(projectDir)
	self := -1
	for i, e := range entries {
		if e.SessionID == sessionID && filepath.Clean(e.FullPath) == sessionPath {
			self = i
			break
		}
	}
	if self < 0 {
		return nil, true
	}

	holds := make([]bool, len(entries))
	forEachParallel(len(entries), func(i int) {
		if i == self {
			holds[i] = true
			return
		}
		id, _ := parser.FirstReplyID(entries[i].FullPath)
		holds[i] = id == firstID
	})
	var family []models.SessionEntry
	for i, e := range entries {
		if holds[i] {
			if i == self {
				self = len(family)
			}
			family = append(family, e)
		}
	}
	if len(family) < 2 {
		return nil, true
	}
	return familyCopies(family, self), true
}

// projectSessions lists a project's sessions as list does, index and all,
// since the index's creation times break ties in attribution. list warns of
// an index it can't read; here it's left out the same way.
func projectSessions(projectDir string) []models.SessionEntry {
	disk, err := parser.DiscoverSessionsFromDisk(projectDir)
	if err != nil {
		return nil
	}
	index, err := parser.ParseSessionsIndex(paths.GetSessionsIndexPath(projectDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		index = nil
	}
	entries, _ := parser.MergeSessionSources(index, disk, projectDir)
	return entries
}

// familyCopies parses a fork family's transcripts and returns the replies
// family[self] holds that another member keeps, or nil when it keeps all of
// its own.
func familyCopies(family []models.SessionEntry, self int) *models.CopiedReplies {
	parsed := make([]*parser.ParseResult, len(family))
	forEachParallel(len(family), func(i int) {
		parsed[i], _ = parser.ParseJSONLFileWithResult(family[i].FullPath)
	})
	if parsed[self] == nil {
		return nil
	}
	// Only the sessions ahead of this one in list's order can take its
	// replies from it.
	seen := make(map[parser.DedupKey]string)
	for _, i := range attributionOrder(family, parsed, make([]bool, len(family))) {
		if i == self {
			break
		}
		if parsed[i] != nil {
			parser.ExcludeSeenMessages(parsed[i].Messages, seen, family[i].SessionID)
		}
	}

	var copied []models.JSONLMessage
	keepers := map[string]bool{}
	var keeper string
	for _, msg := range parsed[self].Messages {
		if id, ok := parser.KeptBy(msg, seen); ok {
			copied = append(copied, msg)
			if !keepers[id] {
				keepers[id] = true
				keeper = id
			}
		}
	}
	if len(copied) == 0 {
		return nil
	}
	c := &models.CopiedReplies{Messages: len(copied), Sessions: len(keepers)}
	for _, m := range parser.ExtractUsageFromMessages(copied) {
		CalculateMessageCost(&m)
		c.Cost += m.Cost.TotalCost
	}
	if len(keepers) == 1 {
		c.SessionID = keeper
	}
	return c
}
