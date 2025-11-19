package threads

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const cacheVersion = 1

type Cache interface {
	Load(Context) (*Entry, error)
	Save(Context, *Entry) error
}

type CacheManager struct {
	basePath string
}

type Entry struct {
	Version              int                   `json:"version"`
	Repository           string                `json:"repository"`
	PullRequest          int                   `json:"pull_request"`
	ConversationComments []ConversationComment `json:"conversation_comments"`
	ReviewThreads        []ReviewThread        `json:"review_threads"`
	LastConversationSync string                `json:"last_conversation_sync"`
	LastReviewSync       string                `json:"last_review_sync"`
	LastStatusSync       string                `json:"last_status_sync"`
}

func NewCacheManager() (Cache, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(base, "threads")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, err
	}
	return &CacheManager{basePath: path}, nil
}

func (m *CacheManager) Load(ctx Context) (*Entry, error) {
	path := m.cachePath(ctx)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	if entry.Version != cacheVersion {
		return nil, nil
	}
	return &entry, nil
}

func (m *CacheManager) Save(ctx Context, entry *Entry) error {
	entry.Version = cacheVersion
	entry.Repository = fmt.Sprintf("%s/%s", ctx.Owner, ctx.Repo)
	entry.PullRequest = ctx.PullRequest
	payload, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	path := m.cachePath(ctx)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *CacheManager) cachePath(ctx Context) string {
	repoPath := filepath.Join(m.basePath, ctx.Owner, ctx.Repo)
	return filepath.Join(repoPath, fmt.Sprintf("%d.json", ctx.PullRequest))
}

func LatestTimestamp(comments []ConversationComment) string {
	var latest time.Time
	for _, comment := range comments {
		ts := parseTime(comment.UpdatedAt, comment.CreatedAt)
		if ts.After(latest) {
			latest = ts
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.UTC().Format(time.RFC3339)
}

func LatestThreadTimestamp(threads []ReviewThread) string {
	var latest time.Time
	for _, thread := range threads {
		for _, comment := range thread.Comments {
			ts := parseTime(comment.UpdatedAt, comment.CreatedAt)
			if ts.After(latest) {
				latest = ts
			}
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.UTC().Format(time.RFC3339)
}

func parseTime(values ...string) time.Time {
	for _, value := range values {
		if value == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, value); err == nil {
			return ts
		}
	}
	return time.Time{}
}

func nextTimestamp(ts string) string {
	if ts == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return parsed.Add(time.Millisecond).UTC().Format(time.RFC3339)
}
