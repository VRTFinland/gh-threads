package app

import (
	"flag"
	"io"
	"strings"
	"testing"
)

func testFlags() (*flag.FlagSet, *string, *string, *bool) {
	fs := flag.NewFlagSet("gh-threads", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", "", "")
	format := fs.String("format", "summary", "")
	showDiff := fs.Bool("show-diff", false, "")
	return fs, repo, format, showDiff
}

// Flags used to be dropped once a positional argument had been seen, so the
// form the README documents queried the wrong repository in the default format
// and said nothing about it.
func TestParseArgsAcceptsFlagsOnEitherSideOfTheNumber(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "flags first", args: []string{"--repo", "o/r", "--format", "json", "--show-diff", "13533"}},
		{name: "number first", args: []string{"13533", "--repo", "o/r", "--format", "json", "--show-diff"}},
		{name: "number in the middle", args: []string{"--repo", "o/r", "13533", "--format", "json", "--show-diff"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs, repo, format, showDiff := testFlags()

			positional, err := parseArgs(fs, tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(positional) != 1 || positional[0] != "13533" {
				t.Fatalf("expected the pull request number, got %v", positional)
			}
			if *repo != "o/r" {
				t.Fatalf("expected --repo to survive, got %q", *repo)
			}
			if *format != "json" {
				t.Fatalf("expected --format to survive, got %q", *format)
			}
			if !*showDiff {
				t.Fatal("expected --show-diff to survive")
			}
		})
	}
}

func TestParseArgsWithoutPositional(t *testing.T) {
	fs, repo, _, _ := testFlags()

	positional, err := parseArgs(fs, []string{"--repo", "o/r"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(positional) != 0 {
		t.Fatalf("expected no positional argument, got %v", positional)
	}
	if *repo != "o/r" {
		t.Fatalf("expected --repo to be parsed, got %q", *repo)
	}
}

func TestParseArgsRejectsExtraPositionals(t *testing.T) {
	fs, _, _, _ := testFlags()

	_, err := parseArgs(fs, []string{"13533", "14863"})

	if err == nil || !strings.Contains(err.Error(), "expected one pull request number") {
		t.Fatalf("expected a clear error for a second number, got %v", err)
	}
}

func TestParseArgsRejectsUnknownFlag(t *testing.T) {
	fs, _, _, _ := testFlags()

	if _, err := parseArgs(fs, []string{"13533", "--nope"}); err == nil {
		t.Fatal("expected an unknown flag to be rejected, not swallowed")
	}
}

// --hide-diff used to quietly override --show-diff, so a command asking for
// both got the opposite of one of its own flags with nothing said.
func TestDiffOptionRejectsContradictoryFlags(t *testing.T) {
	if _, err := diffOption(true, true); err == nil {
		t.Fatal("expected --show-diff with --hide-diff to be rejected")
	}

	cases := []struct {
		show, hide, want bool
	}{
		{show: true, hide: false, want: true},
		{show: false, hide: true, want: false},
		{show: false, hide: false, want: false},
	}
	for _, tc := range cases {
		got, err := diffOption(tc.show, tc.hide)
		if err != nil {
			t.Fatalf("show=%v hide=%v: unexpected error %v", tc.show, tc.hide, err)
		}
		if got != tc.want {
			t.Fatalf("show=%v hide=%v: got %v, want %v", tc.show, tc.hide, got, tc.want)
		}
	}
}

// The TUI cannot act on these, and parsing them without saying so handed back
// a screen that ignored half the command line.
func TestInteractiveFlagConflict(t *testing.T) {
	for _, name := range []string{"format", "no-colour", "no-color", "no-markdown"} {
		err := interactiveFlagConflict(map[string]bool{name: true})
		if err == nil {
			t.Fatalf("expected --%s to be rejected in interactive mode", name)
		}
		if !strings.Contains(err.Error(), "--"+name) {
			t.Fatalf("expected the error to name --%s, got %q", name, err)
		}
	}

	if err := interactiveFlagConflict(map[string]bool{"status": true, "author": true, "text": true, "show-diff": true}); err != nil {
		t.Fatalf("filters and diff flags are honoured in interactive mode, got %v", err)
	}

	err := interactiveFlagConflict(map[string]bool{"format": true, "no-markdown": true})
	if err == nil || !strings.Contains(err.Error(), "--format") || !strings.Contains(err.Error(), "--no-markdown") {
		t.Fatalf("expected every offender to be named, got %v", err)
	}
}
