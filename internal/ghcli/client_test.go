package ghcli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func clientReturning(payload string, err error) *Client {
	c := &Client{}
	c.exec = func(ctx context.Context, args ...string) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		return []byte(payload), nil
	}
	return c
}

func TestFileLinesTreatsNullObjectAsAbsent(t *testing.T) {
	c := clientReturning(`{"data":{"repository":{"object":null}}}`, nil)

	lines, err := c.FileLines(context.Background(), "o", "r", "abc", "gone.go")

	if err != nil {
		t.Fatalf("a missing blob is not an error, got %v", err)
	}
	if lines != nil {
		t.Fatalf("expected no lines for a missing blob, got %v", lines)
	}
}

func TestFileLinesReturnsBlobText(t *testing.T) {
	c := clientReturning(`{"data":{"repository":{"object":{"__typename":"Blob","text":"one\ntwo\n"}}}}`, nil)

	lines, err := c.FileLines(context.Background(), "o", "r", "abc", "file.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) < 2 || lines[0] != "one" || lines[1] != "two" {
		t.Fatalf("unexpected lines: %q", lines)
	}
}

// A permission or transport failure must not masquerade as an absent file: it
// used to be swallowed by matching "404"/"not found" in the error text, which
// left the user with no code context and no warning.
func TestFileLinesPropagatesRealErrors(t *testing.T) {
	for _, message := range []string{
		"HTTP 404: Not Found (https://api.github.com/graphql)",
		"Could not resolve to a Repository with the name 'o/r'.",
		"API rate limit exceeded",
	} {
		c := clientReturning("", errors.New(message))

		lines, err := c.FileLines(context.Background(), "o", "r", "abc", "file.go")

		if err == nil {
			t.Fatalf("%q: expected the failure to propagate, got lines=%v", message, lines)
		}
		if !strings.Contains(err.Error(), message) {
			t.Fatalf("%q: expected the cause to be preserved, got %v", message, err)
		}
	}
}

func TestFileLinesRejectsMissingArguments(t *testing.T) {
	c := clientReturning(`{"data":{}}`, nil)
	if _, err := c.FileLines(context.Background(), "", "r", "abc", "file.go"); err == nil {
		t.Fatal("expected an error for a missing owner")
	}
}
