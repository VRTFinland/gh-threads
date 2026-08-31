package diff

import "testing"

func TestParseHunkNumbersBothSides(t *testing.T) {
	hunk := "@@ -10,4 +20,5 @@ func f() {\n context\n-gone\n+added\n+also added\n tail"

	lines := ParseHunk(hunk)
	if len(lines) != 6 {
		t.Fatalf("expected every line back, got %d", len(lines))
	}

	want := []Line{
		{Text: "@@ -10,4 +20,5 @@ func f() {", Kind: Header},
		{Text: " context", Kind: Context, Old: 10, New: 20},
		{Text: "-gone", Kind: Removed, Old: 11},
		{Text: "+added", Kind: Added, New: 21},
		{Text: "+also added", Kind: Added, New: 22},
		{Text: " tail", Kind: Context, Old: 12, New: 23},
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("line %d: got %+v, want %+v", i, lines[i], w)
		}
	}
}

// A hunk pasted without its header still has to number its own lines, or a
// comment anchored to the first added line highlights nothing.
func TestParseHunkWithoutHeaderNumbersFromOne(t *testing.T) {
	lines := ParseHunk("+first\n+second\n-removed")

	if lines[0].New != 1 || lines[1].New != 2 {
		t.Fatalf("added lines must number from one, got %+v", lines)
	}
	if lines[2].Old != 1 {
		t.Fatalf("removed lines must number from one, got %+v", lines[2])
	}
	if lines[0].Old != 0 || lines[2].New != 0 {
		t.Fatalf("a line has no position on the side it does not appear on: %+v", lines)
	}
}

// Context lines before any header have no position, so they must not answer to
// a target that happens to share their ordinal.
func TestContextBeforeHeaderHasNoPosition(t *testing.T) {
	lines := ParseHunk(" bare context\n@@ -1,1 +1,1 @@\n still context")

	if lines[0].New != 0 || lines[0].Old != 0 {
		t.Fatalf("expected no position before a header, got %+v", lines[0])
	}
	one := 1
	if lines[0].At(Added, &one) || lines[0].At(Removed, &one) {
		t.Fatal("a line with no position must not match a target")
	}
	if lines[2].New != 1 || lines[2].Old != 1 {
		t.Fatalf("expected the header to seed both counters, got %+v", lines[2])
	}
	if !lines[2].At(Added, &one) || !lines[2].At(Removed, &one) {
		t.Fatalf("expected the seeded line to match on both sides, got %+v", lines[2])
	}
}

func TestParseHunkMalformedHeader(t *testing.T) {
	lines := ParseHunk("@@ garbage @@\n+added\n context")

	if lines[0].Kind != Header {
		t.Fatalf("expected the header to be kept verbatim, got %+v", lines[0])
	}
	if lines[1].New != 1 {
		t.Fatalf("expected numbering to restart at one, got %+v", lines[1])
	}
	if lines[2].Old != 0 {
		t.Fatalf("a header that says nothing about the old side places nothing: %+v", lines[2])
	}
}

func TestParseHunkEmpty(t *testing.T) {
	if lines := ParseHunk(""); lines != nil {
		t.Fatalf("expected nothing for an empty hunk, got %v", lines)
	}
}

func TestAtRejectsNilTarget(t *testing.T) {
	line := Line{Kind: Added, New: 5}
	if line.At(Added, nil) {
		t.Fatal("no target means no match")
	}
	zero := 0
	if line.At(Added, &zero) {
		t.Fatal("line numbers are one-based; zero is not a position")
	}
}
