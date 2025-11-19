package gitremote

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/VRTFinland/gh-threads/internal/ghcli"
)

type Cache struct {
	client    *ghcli.Client
	trees     map[string]map[string]string
	blobLines map[string][]string
	fileLines map[fileKey][]string
}

type fileKey struct {
	commit string
	path   string
}

func New(client *ghcli.Client) *Cache {
	return &Cache{
		client:    client,
		trees:     make(map[string]map[string]string),
		blobLines: make(map[string][]string),
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

	blobSHA, err := c.blobForPath(ctx, owner, repo, commit, path)
	if err != nil || blobSHA == "" {
		return nil, err
	}

	lines, err := c.blobContent(ctx, owner, repo, blobSHA)
	if err != nil {
		return nil, err
	}
	c.fileLines[key] = lines
	return lines, nil
}

func (c *Cache) blobForPath(ctx context.Context, owner, repo, commit, path string) (string, error) {
	tree, ok := c.trees[commit]
	if !ok {
		entries, err := c.client.FetchTree(ctx, owner, repo, commit)
		if err != nil {
			if isNotFound(err) {
				return "", nil
			}
			return "", err
		}
		tree = make(map[string]string, len(entries))
		for _, entry := range entries {
			if entry.Type != "blob" {
				continue
			}
			normalised := strings.TrimPrefix(entry.Path, "./")
			normalised = strings.TrimPrefix(normalised, "/")
			tree[normalised] = entry.SHA
		}
		c.trees[commit] = tree
	}
	target := strings.TrimPrefix(path, "./")
	target = strings.TrimPrefix(target, "/")
	return tree[target], nil
}

func (c *Cache) blobContent(ctx context.Context, owner, repo, sha string) ([]string, error) {
	if lines, ok := c.blobLines[sha]; ok {
		return lines, nil
	}
	blob, err := c.client.FetchBlob(ctx, owner, repo, sha)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(blob.Content)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(decoded), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	c.blobLines[sha] = lines
	return lines, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}
