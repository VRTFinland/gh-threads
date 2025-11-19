package ghcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strings"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) EnsureAvailable() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("GitHub CLI (gh) is required but not installed or not in PATH")
	}
	return nil
}

func (c *Client) RepoSlug(ctx context.Context) (string, error) {
	output, err := c.run(ctx, "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		return "", err
	}
	slug := strings.TrimSpace(output)
	if !strings.Contains(slug, "/") {
		return "", fmt.Errorf("invalid repository slug received from gh: %s", slug)
	}
	return slug, nil
}

func (c *Client) CallGraphQL(ctx context.Context, query string, variables map[string]string, target any) error {
	args := []string{"api", "graphql", "-f", fmt.Sprintf("query=%s", query)}
	if len(variables) > 0 {
		keys := make([]string, 0, len(variables))
		for key := range variables {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			args = append(args, "-F", fmt.Sprintf("%s=%s", key, variables[key]))
		}
	}

	raw, err := c.run(ctx, args...)
	if err != nil {
		return err
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return fmt.Errorf("failed to decode GraphQL response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		messages := make([]string, 0, len(envelope.Errors))
		for _, item := range envelope.Errors {
			if item.Message != "" {
				messages = append(messages, item.Message)
			}
		}
		if len(messages) == 0 {
			messages = append(messages, "GraphQL error")
		}
		return errors.New(strings.Join(messages, "; "))
	}
	if target == nil || len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("failed to decode GraphQL data: %w", err)
	}
	return nil
}

func (c *Client) FetchTree(ctx context.Context, owner, repo, commit string) ([]TreeEntry, error) {
	if owner == "" || repo == "" || commit == "" {
		return nil, errors.New("missing owner, repo, or commit")
	}
	path := fmt.Sprintf("repos/%s/%s/git/trees/%s", owner, repo, commit)
	args := []string{"api", path, "--paginate", "-F", "recursive=1"}
	raw, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tree []TreeEntry `json:"tree"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return payload.Tree, nil
}

func (c *Client) FetchBlob(ctx context.Context, owner, repo, sha string) (*Blob, error) {
	if owner == "" || repo == "" || sha == "" {
		return nil, errors.New("missing owner, repo, or blob")
	}
	path := fmt.Sprintf("repos/%s/%s/git/blobs/%s", owner, repo, sha)
	args := []string{"api", path}
	raw, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var blob Blob
	if err := json.Unmarshal([]byte(raw), &blob); err != nil {
		return nil, err
	}
	if blob.Encoding != "base64" {
		return nil, fmt.Errorf("unsupported blob encoding: %s", blob.Encoding)
	}
	return &blob, nil
}

func (c *Client) HasIssueCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error) {
	if since == "" {
		return true, nil
	}
	endpoint := fmt.Sprintf("repos/%s/%s/issues/%d/comments", owner, repo, prNumber)
	return c.hasUpdates(ctx, endpoint, since)
}

func (c *Client) HasReviewCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error) {
	if since == "" {
		return true, nil
	}
	endpoint := fmt.Sprintf("repos/%s/%s/pulls/%d/comments", owner, repo, prNumber)
	return c.hasUpdates(ctx, endpoint, since)
}

func (c *Client) PostREST(ctx context.Context, method, path string, body map[string]string, target any) error {
	args := []string{"api", "--method", method, path}
	for key, value := range body {
		args = append(args, "-f", fmt.Sprintf("%s=%s", key, value))
	}
	raw, err := c.run(ctx, args...)
	if err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal([]byte(raw), target)
}

func (c *Client) hasUpdates(ctx context.Context, endpoint string, since string) (bool, error) {
	query := fmt.Sprintf("%s?per_page=1&since=%s&direction=asc&sort=updated", endpoint, url.QueryEscape(since))
	args := []string{"api", query}
	raw, err := c.run(ctx, args...)
	if err != nil {
		return false, err
	}
	var payload []struct {
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return false, err
	}
	return len(payload) > 0, nil
}

func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	output, err := c.runBytes(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (c *Client) runBytes(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return stdout.Bytes(), nil
}

type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type Blob struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type PullRequestSummary struct {
	Number int    `json:"number"`
	State  string `json:"state"`
}

func (c *Client) ListOpenPullRequestsByHead(ctx context.Context, owner, repo, branch string) ([]PullRequestSummary, error) {
	if owner == "" || repo == "" || branch == "" {
		return nil, errors.New("missing owner, repo, or branch")
	}
	args := []string{
		"pr", "list",
		"--repo", fmt.Sprintf("%s/%s", owner, repo),
		"--state", "open",
		"--head", branch,
		"--json", "number,state",
	}
	raw, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var prs []PullRequestSummary
	if err := json.Unmarshal([]byte(raw), &prs); err != nil {
		return nil, fmt.Errorf("failed to parse gh pr list output: %w", err)
	}
	return prs, nil
}
