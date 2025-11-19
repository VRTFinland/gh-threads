package threads

import "strings"

func FilterConversationComments(comments []ConversationComment, author string) []ConversationComment {
	author = strings.TrimSpace(author)
	if author == "" {
		return comments
	}
	filtered := make([]ConversationComment, 0, len(comments))
	for _, comment := range comments {
		if strings.EqualFold(comment.Author, author) {
			filtered = append(filtered, comment)
		}
	}
	return filtered
}

func FilterReviewThreads(threads []ReviewThread, author string, status StatusFilter) []ReviewThread {
	author = strings.TrimSpace(author)
	needAuthor := author != ""
	filtered := make([]ReviewThread, 0, len(threads))
	for _, thread := range threads {
		if status == StatusResolved && !thread.IsResolved {
			continue
		}
		if status == StatusUnresolved && thread.IsResolved {
			continue
		}
		comments := thread.Comments
		if needAuthor {
			tmp := make([]ThreadComment, 0, len(comments))
			for _, comment := range comments {
				if strings.EqualFold(comment.Author, author) {
					tmp = append(tmp, comment)
				}
			}
			if len(tmp) == 0 {
				continue
			}
			comments = tmp
		}
		filtered = append(filtered, ReviewThread{
			ThreadID:     thread.ThreadID,
			Path:         thread.Path,
			Line:         thread.Line,
			OriginalLine: thread.OriginalLine,
			StartLine:    thread.StartLine,
			IsResolved:   thread.IsResolved,
			IsOutdated:   thread.IsOutdated,
			Comments:     comments,
		})
	}
	return filtered
}
