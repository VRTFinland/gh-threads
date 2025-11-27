package threads

import "strings"

func FilterConversationComments(comments []ConversationComment, author, text string) []ConversationComment {
	author = strings.TrimSpace(author)
	text = strings.TrimSpace(text)
	needText := text != ""
	textLower := strings.ToLower(text)
	filtered := make([]ConversationComment, 0, len(comments))
	for _, comment := range comments {
		if author != "" && !strings.EqualFold(comment.Author, author) {
			continue
		}
		if needText && !strings.Contains(strings.ToLower(comment.Body), textLower) {
			continue
		}
		filtered = append(filtered, comment)
	}
	return filtered
}

func FilterReviewThreads(threads []ReviewThread, author string, status StatusFilter, text string) []ReviewThread {
	author = strings.TrimSpace(author)
	text = strings.TrimSpace(text)
	needAuthor := author != ""
	needText := text != ""
	textLower := strings.ToLower(text)
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
		if needText && !matchesThreadText(thread.Path, comments, textLower) {
			continue
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

func matchesThreadText(path string, comments []ThreadComment, textLower string) bool {
	if strings.Contains(strings.ToLower(path), textLower) {
		return true
	}
	for _, comment := range comments {
		if strings.Contains(strings.ToLower(comment.Body), textLower) {
			return true
		}
	}
	return false
}
