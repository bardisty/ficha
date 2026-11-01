package analyzer

import (
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
)

// FindCopies returns the replies in a session's transcript that list and
// summary count under another session in its project, priced, and which
// session that is. It returns nil when there are none. checked is as
// FindFamily's.
func FindCopies(sessionPath, sessionID string) (copies *models.CopiedReplies, checked bool) {
	f, checked := FindFamily(sessionPath, sessionID)
	if f == nil {
		return nil, checked
	}
	return f.Copies, checked
}

// ForkFamily is the sessions in a project that begin with one session's
// first reply, and that session's copies of their replies. Copies is nil
// when the session keeps all of its own.
type ForkFamily struct {
	Copies *models.CopiedReplies

	members []models.SessionEntry
	self    int
	// indexed marks the members whose Created came from sessions-index.json.
	// The rest take their file's mtime, which moves on every write.
	indexed []bool
	// earliest is each member's first message time, from the parse that
	// found Copies. Appends don't change it.
	earliest []time.Time
	// ahead is the members ahead of self in attribution order, which alone
	// decide Copies.
	ahead []int
}

// FindFamily returns the session's fork family, or nil when no other
// session in its project shares its first reply.
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
func FindFamily(sessionPath, sessionID string) (family *ForkFamily, checked bool) {
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

	entries, index := projectSessions(projectDir)
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
	f := &ForkFamily{}
	for i, e := range entries {
		if holds[i] {
			if i == self {
				f.self = len(f.members)
			}
			f.members = append(f.members, e)
			f.indexed = append(f.indexed, index[e.SessionID])
		}
	}
	if len(f.members) < 2 {
		return nil, true
	}
	f.find()
	return f, true
}

// Includes reports whether sessionID is in the family.
func (f *ForkFamily) Includes(sessionID string) bool {
	for _, e := range f.members {
		if e.SessionID == sessionID {
			return true
		}
	}
	return false
}

// Recheck returns the family with its copies found again, or nil when the
// members ahead of this session are as they were, and so are the copies.
// Without sessions-index.json a fork and its original tie on their first
// message and go by file mtime, so a write to either can move the credit.
// A recheck stats each member's file, and parses the family only when the
// order moved.
func (f *ForkFamily) Recheck() *ForkFamily {
	members := slices.Clone(f.members)
	for i := range members {
		info, err := os.Stat(members[i].FullPath)
		if err != nil {
			continue
		}
		members[i].Modified = info.ModTime()
		if !f.indexed[i] {
			members[i].Created = info.ModTime()
		}
	}
	if slices.Equal(aheadOf(orderByEarliest(members, f.earliest), f.self), f.ahead) {
		return nil
	}
	next := &ForkFamily{members: members, self: f.self, indexed: f.indexed}
	next.find()
	return next
}

// find parses the family's transcripts and sets Copies, earliest and ahead.
func (f *ForkFamily) find() {
	parsed := make([]*parser.ParseResult, len(f.members))
	forEachParallel(len(f.members), func(i int) {
		parsed[i], _ = parser.ParseJSONLFileWithResult(f.members[i].FullPath)
	})
	f.earliest = make([]time.Time, len(f.members))
	for i, p := range parsed {
		if p != nil {
			f.earliest[i] = earliestMessageTime(p.Messages)
		}
	}
	f.ahead = aheadOf(orderByEarliest(f.members, f.earliest), f.self)
	f.Copies = familyCopies(f.members, parsed, f.ahead, f.self)
}

// aheadOf is the part of order before self.
func aheadOf(order []int, self int) []int {
	return order[:slices.Index(order, self)]
}

// projectSessions lists a project's sessions as list does, index and all,
// since the index's creation times break ties in attribution, and the IDs
// the index holds. An index it can't read is left out, as list leaves it
// out after warning.
func projectSessions(projectDir string) ([]models.SessionEntry, map[string]bool) {
	disk, err := parser.DiscoverSessionsFromDisk(projectDir)
	if err != nil {
		return nil, nil
	}
	index, _ := parser.ParseSessionsIndex(paths.GetSessionsIndexPath(projectDir))
	entries, _ := parser.MergeSessionSources(index, disk, projectDir)
	indexed := map[string]bool{}
	if index != nil {
		for _, e := range index.Entries {
			indexed[e.SessionID] = true
		}
	}
	return entries, indexed
}

// familyCopies returns the replies family[self] holds that a member ahead
// of it keeps, or nil when it keeps all of its own. parsed is the family's
// parses, nil where one failed.
func familyCopies(family []models.SessionEntry, parsed []*parser.ParseResult, ahead []int, self int) *models.CopiedReplies {
	if parsed[self] == nil {
		return nil
	}
	seen := make(map[parser.DedupKey]string)
	for _, i := range ahead {
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
