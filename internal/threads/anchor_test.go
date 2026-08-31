package threads

import "testing"

func ptr(v int) *int { return &v }

func TestThreadCommentAnchor(t *testing.T) {
	cases := []struct {
		name    string
		comment ThreadComment
		prefer  LineSpace
		want    LineAnchor
	}{
		{
			name: "prefers the original pair over the current one",
			comment: ThreadComment{
				Line: ptr(118), StartLine: ptr(116),
				OriginalLine: ptr(120), OriginalStartLine: ptr(115),
			},
			prefer: OriginalSpace,
			want:   LineAnchor{Start: 115, End: 120, Space: OriginalSpace},
		},
		{
			name:    "outdated comment with no current lines",
			comment: ThreadComment{OriginalLine: ptr(120), OriginalStartLine: ptr(115)},
			prefer:  OriginalSpace,
			want:    LineAnchor{Start: 115, End: 120, Space: OriginalSpace},
		},
		{
			name:    "falls back to the current pair without an original line",
			comment: ThreadComment{Line: ptr(40), StartLine: ptr(38)},
			prefer:  OriginalSpace,
			want:    LineAnchor{Start: 38, End: 40, Space: CurrentSpace},
		},
		{
			name:    "single line comment",
			comment: ThreadComment{OriginalLine: ptr(12)},
			prefer:  OriginalSpace,
			want:    LineAnchor{Start: 12, End: 12, Space: OriginalSpace},
		},
		{
			name: "derives the start from the span for a cached comment",
			comment: ThreadComment{
				Line: ptr(118), StartLine: ptr(116),
				OriginalLine: ptr(120),
			},
			prefer: OriginalSpace,
			want:   LineAnchor{Start: 118, End: 120, Space: OriginalSpace},
		},
		{
			name:    "no line information at all",
			comment: ThreadComment{},
			prefer:  OriginalSpace,
			want:    LineAnchor{},
		},
		{
			name: "the current space can be preferred instead",
			comment: ThreadComment{
				Line: ptr(118), StartLine: ptr(116),
				OriginalLine: ptr(120), OriginalStartLine: ptr(115),
			},
			prefer: CurrentSpace,
			want:   LineAnchor{Start: 116, End: 118, Space: CurrentSpace},
		},
		{
			name:    "a reversed pair is ordered",
			comment: ThreadComment{OriginalLine: ptr(10), OriginalStartLine: ptr(14)},
			prefer:  OriginalSpace,
			want:    LineAnchor{Start: 10, End: 14, Space: OriginalSpace},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.comment.Anchor(tc.prefer); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReviewThreadAnchor(t *testing.T) {
	cases := []struct {
		name   string
		thread ReviewThread
		want   LineAnchor
	}{
		{
			name:   "current multi-line range",
			thread: ReviewThread{Line: ptr(120), StartLine: ptr(116)},
			want:   LineAnchor{Start: 116, End: 120, Space: CurrentSpace},
		},
		{
			name:   "current single line",
			thread: ReviewThread{Line: ptr(120)},
			want:   LineAnchor{Start: 120, End: 120, Space: CurrentSpace},
		},
		{
			name: "outdated thread uses the original pair whole",
			thread: ReviewThread{
				StartLine:    ptr(116), // stale, belongs to the current diff
				OriginalLine: ptr(120), OriginalStartLine: ptr(115),
			},
			want: LineAnchor{Start: 115, End: 120, Space: OriginalSpace},
		},
		{
			name:   "outdated single line",
			thread: ReviewThread{OriginalLine: ptr(12)},
			want:   LineAnchor{Start: 12, End: 12, Space: OriginalSpace},
		},
		{
			name:   "no line information",
			thread: ReviewThread{},
			want:   LineAnchor{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.thread.Anchor(CurrentSpace); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLineAnchorString(t *testing.T) {
	cases := []struct {
		anchor LineAnchor
		want   string
	}{
		{LineAnchor{Start: 116, End: 120}, "116-120"},
		{LineAnchor{Start: 120, End: 120}, "120"},
		{LineAnchor{}, ""},
	}
	for _, tc := range cases {
		if got := tc.anchor.String(); got != tc.want {
			t.Fatalf("%+v: got %q, want %q", tc.anchor, got, tc.want)
		}
	}
}

func TestLineAnchorZeroValueIsNotValid(t *testing.T) {
	if (LineAnchor{}).Valid() {
		t.Fatal("the zero anchor must not read as a real position")
	}
	if !(LineAnchor{Start: 1, End: 1}).Valid() {
		t.Fatal("line 1 is a real position")
	}
}
