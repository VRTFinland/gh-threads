package threads

import "testing"

func TestFilterConversationComments(t *testing.T) {
	comments := []ConversationComment{
		{ID: "1", Author: "alice", Body: "First comment"},
		{ID: "2", Author: "bob", Body: "Second interesting note"},
	}

	tests := []struct {
		name    string
		author  string
		text    string
		wantIDs []string
	}{
		{name: "no filters", wantIDs: []string{"1", "2"}},
		{name: "author filter", author: "alice", wantIDs: []string{"1"}},
		{name: "text filter", text: "interesting", wantIDs: []string{"2"}},
		{name: "both filters", author: "bob", text: "second", wantIDs: []string{"2"}},
		{name: "no matches", author: "alice", text: "second", wantIDs: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterConversationComments(comments, tt.author, tt.text)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("expected %d comments, got %d", len(tt.wantIDs), len(got))
			}
			for i, c := range got {
				if c.ID != tt.wantIDs[i] {
					t.Fatalf("comment index %d: expected ID %s, got %s", i, tt.wantIDs[i], c.ID)
				}
			}
		})
	}
}

func TestFilterReviewThreads(t *testing.T) {
	data := []ReviewThread{
		{
			ThreadID:   "t1",
			Path:       "pkg/foo.go",
			IsResolved: false,
			Comments: []ThreadComment{
				{ID: "c1", Author: "alice", Body: "Please fix the panic"},
				{ID: "c2", Author: "bob", Body: "Looks fine"},
			},
		},
		{
			ThreadID:   "t2",
			Path:       "pkg/bar.go",
			IsResolved: true,
			Comments: []ThreadComment{
				{ID: "c3", Author: "carol", Body: "nit"},
			},
		},
	}

	tests := []struct {
		name             string
		author           string
		status           StatusFilter
		text             string
		wantThreadIDs    []string
		wantCommentCount map[string]int
	}{
		{
			name:          "no filters returns all",
			status:        StatusAll,
			wantThreadIDs: []string{"t1", "t2"},
			wantCommentCount: map[string]int{
				"t1": 2,
				"t2": 1,
			},
		},
		{
			name:          "status unresolved",
			status:        StatusUnresolved,
			wantThreadIDs: []string{"t1"},
			wantCommentCount: map[string]int{
				"t1": 2,
			},
		},
		{
			name:          "author filter trims comments",
			author:        "alice",
			status:        StatusAll,
			wantThreadIDs: []string{"t1"},
			wantCommentCount: map[string]int{
				"t1": 1,
			},
		},
		{
			name:          "text filter matches path",
			text:          "bar",
			status:        StatusAll,
			wantThreadIDs: []string{"t2"},
			wantCommentCount: map[string]int{
				"t2": 1,
			},
		},
		{
			name:          "author and text filters both required",
			author:        "alice",
			text:          "panic",
			status:        StatusAll,
			wantThreadIDs: []string{"t1"},
			wantCommentCount: map[string]int{
				"t1": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterReviewThreads(data, tt.author, tt.status, tt.text)
			if len(got) != len(tt.wantThreadIDs) {
				t.Fatalf("expected %d threads, got %d", len(tt.wantThreadIDs), len(got))
			}
			for i, thread := range got {
				if thread.ThreadID != tt.wantThreadIDs[i] {
					t.Fatalf("thread index %d: expected ID %s, got %s", i, tt.wantThreadIDs[i], thread.ThreadID)
				}
				if tt.wantCommentCount != nil {
					if gotCount := len(thread.Comments); gotCount != tt.wantCommentCount[thread.ThreadID] {
						t.Fatalf("thread %s: expected %d comments, got %d", thread.ThreadID, tt.wantCommentCount[thread.ThreadID], gotCount)
					}
				}
			}
		})
	}
}
