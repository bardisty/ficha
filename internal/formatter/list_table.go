package formatter

import (
	"fmt"
	"strings"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/styles"
)

// FormatSessionListTable formats a list of sessions as a table
func FormatSessionListTable(entries []models.SessionEntry, noColor bool) string {
	var sb strings.Builder
	const width = 76

	// Header panel
	sessionWord := "sessions"
	if len(entries) == 1 {
		sessionWord = "session"
	}
	headerText := fmt.Sprintf("%d %s", len(entries), sessionWord)

	if noColor {
		// Plain header panel
		sb.WriteString(styles.AsciiCorner)
		sb.WriteString(strings.Repeat(styles.AsciiHorizontal, width-2))
		sb.WriteString(styles.AsciiCorner)
		sb.WriteString("\n")

		sb.WriteString(styles.AsciiVertical)
		sb.WriteString("  ")
		sb.WriteString(headerText)
		sb.WriteString(strings.Repeat(" ", width-6-len(headerText)))
		sb.WriteString("  ")
		sb.WriteString(styles.AsciiVertical)
		sb.WriteString("\n")

		sb.WriteString(styles.AsciiCorner)
		sb.WriteString(strings.Repeat(styles.AsciiHorizontal, width-2))
		sb.WriteString(styles.AsciiCorner)
		sb.WriteString("\n\n")
	} else {
		// Styled header panel
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopRight))
		sb.WriteString("\n")

		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("  ")
		sb.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("%d", len(entries))))
		sb.WriteString(fmt.Sprintf(" %s", sessionWord))
		sb.WriteString(strings.Repeat(" ", width-6-len(headerText)))
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("\n")

		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomRight))
		sb.WriteString("\n\n")
	}

	// Column headers
	colHeader := fmt.Sprintf("%-36s  %8s  %7s  %s", "Session ID", "Messages", "Agents", "Modified")
	if noColor {
		sb.WriteString(colHeader + "\n")
		sb.WriteString(strings.Repeat("-", width) + "\n")
	} else {
		sb.WriteString(dimStyle.Render(colHeader) + "\n")
		sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, width)) + "\n")
	}

	// Session rows
	for _, entry := range entries {
		modified := entry.Modified.Format("2006-01-02 15:04")
		agentStr := "-"
		if entry.AgentCount > 0 {
			agentStr = fmt.Sprintf("%d (%d)", entry.AgentCount, entry.AgentMessageCount)
		}

		// Truncate session ID to fit (first 8 chars is usually enough to identify)
		shortID := entry.SessionID
		if len(shortID) > 36 {
			shortID = shortID[:33] + "..."
		}

		if noColor {
			sb.WriteString(fmt.Sprintf("%-36s  %8d  %7s  %s\n",
				shortID,
				entry.MessageCount,
				agentStr,
				modified))
		} else {
			// Dim the session ID, highlight message count if > 0
			var msgStr string
			if entry.MessageCount > 0 {
				msgStr = fmt.Sprintf("%8d", entry.MessageCount)
			} else {
				msgStr = dimStyle.Render(fmt.Sprintf("%8d", entry.MessageCount))
			}
			sb.WriteString(fmt.Sprintf("%s  %s  %7s  %s\n",
				dimStyle.Render(fmt.Sprintf("%-36s", shortID)),
				msgStr,
				agentStr,
				dimStyle.Render(modified)))
		}
	}

	// Footer separator
	sb.WriteString("\n")
	if noColor {
		sb.WriteString(strings.Repeat("-", width))
	} else {
		sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, width)))
	}

	return sb.String()
}
