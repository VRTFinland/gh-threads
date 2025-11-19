package gitlocal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

type Repo struct {
	available bool
}

func Detect(ctx context.Context, owner, repo string) *Repo {
	if _, err := run(ctx, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil
	}

	remoteURL, err := run(ctx, "remote", "get-url", "origin")
	if err != nil {
		return nil
	}

	slug, ok := normaliseSlug(remoteURL)
	if !ok {
		return nil
	}

	expected := fmt.Sprintf("%s/%s", owner, repo)
	if !strings.EqualFold(slug, expected) {
		return nil
	}

	return &Repo{available: true}
}

func (r *Repo) Available() bool {
	return r != nil && r.available
}

func (r *Repo) FileLines(ctx context.Context, commit, path string) ([]string, error) {
	if !r.Available() {
		return nil, errors.New("local repository unavailable")
	}
	expr := fmt.Sprintf("%s:%s", commit, path)
	output, err := run(ctx, "show", expr)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(output, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n"), nil
}

func normaliseSlug(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if strings.HasPrefix(raw, "git@") {
		at := strings.Index(raw, "@")
		colon := strings.Index(raw, ":")
		if at == -1 || colon == -1 || colon <= at {
			return "", false
		}
		host := raw[at+1 : colon]
		if !isGitHubHost(host) {
			return "", false
		}
		path := strings.Trim(raw[colon+1:], "/")
		path = strings.TrimSuffix(path, ".git")
		if path == "" {
			return "", false
		}
		return path, true
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		if !isGitHubHost(parsed.Host) {
			return "", false
		}
		path := strings.Trim(parsed.Path, "/")
		path = strings.TrimSuffix(path, ".git")
		if path == "" {
			return "", false
		}
		return path, true
	}
	return "", false
}

func isGitHubHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "github.com" || host == "www.github.com"
}

func run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func HasGitHubOrigin(ctx context.Context) bool {
	if _, err := run(ctx, "rev-parse", "--is-inside-work-tree"); err != nil {
		return false
	}
	remoteURL, err := run(ctx, "remote", "get-url", "origin")
	if err != nil {
		return false
	}
	_, ok := normaliseSlug(remoteURL)
	return ok
}

func CurrentBranch(ctx context.Context) (string, error) {
	branch, err := run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return branch, nil
}
