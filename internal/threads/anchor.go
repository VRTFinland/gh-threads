package threads

import "strconv"

// LineSpace names the revision a line number is measured in. GitHub reports
// every anchor twice: Line and StartLine are positions in the pull request's
// current diff, OriginalLine and OriginalStartLine positions in the commit the
// comment was written against. The two drift apart as the branch moves, so a
// number taken from one space says nothing about the other, and an anchor must
// take both of its ends from the same one.
type LineSpace int

const (
	CurrentSpace LineSpace = iota
	OriginalSpace
)

// SnippetSpace is the space a HistoricalSnippet is cut in. attachHistoricalSnippets
// reads the file at the comment's original commit, so anything drawn over a
// snippet -- a highlighted line, the lines a suggestion replaces -- has to be
// measured in the same space the snippet was cut in. Both sides name this
// constant so they cannot drift apart.
const SnippetSpace = OriginalSpace

// LineAnchor is the run of lines a comment or thread hangs off, taken whole
// from one coordinate space. Start is never past End, and a single-line anchor
// has Start == End. The zero value means "no anchor at all": GitHub reports no
// lines for some comments, and since line numbers are one-based, End == 0
// cannot be a real position.
type LineAnchor struct {
	Start int
	End   int
	Space LineSpace
}

// Valid reports whether the anchor points at real lines.
func (a LineAnchor) Valid() bool { return a.End > 0 }

// String renders the anchor the way GitHub writes it: "116-120", or "120" for a
// single line. An anchor with no lines renders as the empty string, leaving the
// placeholder to the caller that knows its own display.
func (a LineAnchor) String() string {
	if !a.Valid() {
		return ""
	}
	if a.Start != a.End {
		return strconv.Itoa(a.Start) + "-" + strconv.Itoa(a.End)
	}
	return strconv.Itoa(a.End)
}

// Anchor returns the comment's anchor, taken from prefer when that space
// reports an end line and from the other space otherwise.
func (c ThreadComment) Anchor(prefer LineSpace) LineAnchor {
	return pickAnchor(prefer,
		linePair{start: c.StartLine, end: c.Line},
		linePair{start: c.OriginalStartLine, end: c.OriginalLine})
}

// Anchor returns the thread's anchor, taken from prefer when that space reports
// an end line and from the other space otherwise.
func (t ReviewThread) Anchor(prefer LineSpace) LineAnchor {
	return pickAnchor(prefer,
		linePair{start: t.StartLine, end: t.Line},
		linePair{start: t.OriginalStartLine, end: t.OriginalLine})
}

// linePair is one space's raw pair, either end of which GitHub may leave unset.
type linePair struct {
	start *int
	end   *int
}

// span is the anchor's length, or zero when this pair does not describe one.
func (p linePair) span() int {
	if p.start == nil || p.end == nil || *p.end < *p.start {
		return 0
	}
	return *p.end - *p.start
}

func pickAnchor(prefer LineSpace, current, original linePair) LineAnchor {
	pairs := [2]struct {
		space LineSpace
		pair  linePair
	}{
		{CurrentSpace, current},
		{OriginalSpace, original},
	}
	if prefer == OriginalSpace {
		pairs[0], pairs[1] = pairs[1], pairs[0]
	}
	for _, p := range pairs {
		if anchor := newAnchor(p.space, p.pair, current.span()); anchor.Valid() {
			return anchor
		}
	}
	return LineAnchor{}
}

// newAnchor builds an ordered anchor from one space's pair. A pair with no
// start falls back to span, the anchor's length recovered from the current
// pair: how many lines a comment covers is a property of the comment, not of
// the commit, and GitHub leaves originalStartLine unset on some multi-line
// comments. Without either, the anchor is the single end line.
func newAnchor(space LineSpace, pair linePair, span int) LineAnchor {
	if pair.end == nil {
		return LineAnchor{}
	}
	end := *pair.end
	start := end
	switch {
	case pair.start != nil:
		start = *pair.start
	case span > 0:
		start = end - span
	}
	if start > end {
		start, end = end, start
	}
	return LineAnchor{Start: start, End: end, Space: space}
}
