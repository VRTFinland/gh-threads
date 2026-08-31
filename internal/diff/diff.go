// Package diff reads the unified diff hunks GitHub attaches to review
// comments. Tracking which line of which file a hunk line lands on is fiddly
// enough -- counters seeded from the header, restarted when a hunk arrives
// without one, advanced by one side or both -- that the summary renderer and
// the TUI having a copy each meant two places to get it wrong.
package diff

import (
	"strconv"
	"strings"
)

// LineKind classifies one line of a hunk.
type LineKind int

const (
	Context LineKind = iota
	Added
	Removed
	Header
)

// Line is one line of a hunk, with the position it occupies in each side of
// the diff. New and Old are zero where the line has no position on that side:
// an added line has no place in the old file, a removed line none in the new,
// and neither side is placed until a hunk header says where the hunk starts.
type Line struct {
	Text string
	Kind LineKind
	New  int
	Old  int
}

// At reports whether the line sits at target on the given side, which no line
// without a position on that side ever does.
func (l Line) At(side LineKind, target *int) bool {
	pos := l.New
	if side == Removed {
		pos = l.Old
	}
	return pos > 0 && target != nil && pos == *target
}

// ParseHunk splits a hunk into its lines. Input is taken as written: callers
// that want surrounding blank lines gone should trim before calling.
func ParseHunk(hunk string) []Line {
	if hunk == "" {
		return nil
	}
	raw := strings.Split(strings.ReplaceAll(hunk, "\r", ""), "\n")
	lines := make([]Line, 0, len(raw))

	// Zero means "no position yet": a hunk that never declares a header still
	// numbers its own added and removed lines from one.
	var newLine, oldLine int
	var hasNew, hasOld bool

	for _, text := range raw {
		switch {
		case strings.HasPrefix(text, "@@"):
			if parts := strings.Fields(text); len(parts) >= 3 {
				oldLine, hasOld = hunkStart(parts[1])
				newLine, hasNew = hunkStart(parts[2])
			}
			lines = append(lines, Line{Text: text, Kind: Header})
		case strings.HasPrefix(text, "+"):
			hasNew = true
			newLine++
			lines = append(lines, Line{Text: text, Kind: Added, New: newLine})
		case strings.HasPrefix(text, "-"):
			hasOld = true
			oldLine++
			lines = append(lines, Line{Text: text, Kind: Removed, Old: oldLine})
		default:
			line := Line{Text: text, Kind: Context}
			if hasNew {
				newLine++
				line.New = newLine
			}
			if hasOld {
				oldLine++
				line.Old = oldLine
			}
			lines = append(lines, line)
		}
	}
	return lines
}

// hunkStart reads "+12,7" or "-12" as the line before the hunk's first line,
// so the counters can simply be advanced as the lines are read.
func hunkStart(meta string) (int, bool) {
	meta = strings.TrimSpace(meta)
	if meta == "" {
		return 0, false
	}
	if meta[0] == '+' || meta[0] == '-' {
		meta = meta[1:]
	}
	if idx := strings.Index(meta, ","); idx != -1 {
		meta = meta[:idx]
	}
	value, err := strconv.Atoi(meta)
	if err != nil {
		return 0, false
	}
	return value - 1, true
}
