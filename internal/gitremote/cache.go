package gitremote

import (
	"context"
	"errors"

	"github.com/VRTFinland/gh-threads/internal/ghcli"
)

type Cache struct {
	client    *ghcli.Client
	fileLines map[fileKey][]string
}

type fileKey struct {
	commit string
	path   string
}

func New(client *ghcli.Client) *Cache {
	return &Cache{
		client:    client,
		fileLines: make(map[fileKey][]string),
	}
}

func (c *Cache) GetLines(ctx context.Context, owner, repo, commit, path string) ([]string, error) {
	if commit == "" || path == "" {
		return nil, errors.New("invalid commit or path")
	}
	key := fileKey{commit: commit, path: path}
	if lines, ok := c.fileLines[key]; ok {
		return lines, nil
	}

	lines, err := c.client.FileLines(ctx, owner, repo, commit, path)
	if err != nil {
		return nil, err
	}
	c.fileLines[key] = lines
	return lines, nil
}
