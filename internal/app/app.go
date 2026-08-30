package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/VRTFinland/gh-threads/internal/ghcli"
	"github.com/VRTFinland/gh-threads/internal/gitlocal"
	"github.com/VRTFinland/gh-threads/internal/interactive"
	"github.com/VRTFinland/gh-threads/internal/render"
	"github.com/VRTFinland/gh-threads/internal/threads"
)

type App struct {
	client  *ghcli.Client
	out     io.Writer
	errOut  io.Writer
	outFile *os.File
}

func New() *App {
	return &App{
		client:  ghcli.NewClient(),
		out:     os.Stdout,
		errOut:  os.Stderr,
		outFile: os.Stdout,
	}
}

func (a *App) Run(ctx context.Context, args []string) error {
	if err := a.client.EnsureAvailable(); err != nil {
		return err
	}

	fs := flag.NewFlagSet("gh-threads", flag.ContinueOnError)
	fs.SetOutput(a.errOut)

	repoFlag := fs.String("repo", "", "Repository in owner/name format. Defaults to the current repository detected via gh.")
	formatFlag := fs.String("format", "summary", "Output format: json or summary (default).")
	statusFlag := fs.String("status", "all", "Filter review threads by resolution state: all, resolved, or unresolved.")
	authorFlag := fs.String("author", "", "Filter comments by GitHub login.")
	textFlag := fs.String("text", "", "Filter threads by matching file path or comment body.")
	showDiffFlag := fs.Bool("show-diff", false, "Include diff context in summary output.")
	hideDiffFlag := fs.Bool("hide-diff", false, "Hide diff context in summary output.")
	noColourFlag := fs.Bool("no-colour", false, "Disable coloured terminal output.")
	noColorFlag := fs.Bool("no-color", false, "Disable colored terminal output.")
	noMarkdownFlag := fs.Bool("no-markdown", false, "Disable markdown rendering in summary mode.")
	refreshCacheFlag := fs.Bool("refresh-cache", false, "Force refresh of cached data.")
	interactiveFlag := fs.Bool("interactive", false, "Run in interactive mode (experimental).")
	fs.BoolVar(interactiveFlag, "i", false, "Run in interactive mode (experimental).")

	fs.Usage = func() {
		fmt.Fprintf(a.errOut, "Usage: gh threads [options] <pull_request_number>\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	repoSlug := strings.TrimSpace(*repoFlag)
	if repoSlug == "" {
		var err error
		repoSlug, err = a.client.RepoSlug(ctx)
		if err != nil {
			return fmt.Errorf("failed to determine repository: %w", err)
		}
	}

	owner, repoName, err := splitRepoSlug(repoSlug)
	if err != nil {
		return err
	}

	trailing := fs.Args()
	if len(trailing) == 0 {
		if !gitlocal.HasGitHubOrigin(ctx) {
			fs.Usage()
			return errors.New("pull request number is required when origin is not a GitHub repository")
		}
		prNumber, err := a.detectPullRequestFromBranch(ctx, owner, repoName)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.errOut, "Detected open pull request #%d from the current branch.\n", prNumber)
		trailing = []string{strconv.Itoa(prNumber)}
	}

	pullRequest, err := strconv.Atoi(trailing[0])
	if err != nil || pullRequest <= 0 {
		return fmt.Errorf("invalid pull request number: %s", trailing[0])
	}

	format, err := parseOutputFormat(*formatFlag)
	if err != nil {
		return err
	}

	status, err := parseStatusFilter(*statusFlag)
	if err != nil {
		return err
	}

	showDiff := *showDiffFlag
	if *hideDiffFlag {
		showDiff = false
	}

	noColour := *noColourFlag || *noColorFlag
	markdownEnabled := !*noMarkdownFlag

	colourEnabled := !noColour && isTerminal(a.outFile)

	ghContext := threads.Context{
		Owner:       owner,
		Repo:        repoName,
		PullRequest: pullRequest,
	}

	localRepo := gitlocal.Detect(ctx, ghContext.Owner, ghContext.Repo)

	cacheManager, err := threads.NewCacheManager()
	if err != nil {
		return fmt.Errorf("failed to initialise cache: %w", err)
	}

	service := threads.NewService(a.client, localRepo, cacheManager, a.errOut)

	includeHistory := true

	conversationComments, reviewThreads, err := service.FetchData(ctx, ghContext, includeHistory, *refreshCacheFlag)
	if err != nil {
		return err
	}

	if *interactiveFlag {
		prInfo, err := service.FetchPullRequestInfo(ctx, ghContext)
		if err != nil {
			return fmt.Errorf("failed to fetch pull request info: %w", err)
		}
		cfg := interactive.ProgramConfig{
			Conversation: conversationComments,
			Threads:      reviewThreads,
			Service:      service,
			Info:         prInfo,
			Context:      ghContext,
			Ctx:          ctx,
			Refresh: func(force bool) (threads.PullRequestInfo, []threads.ConversationComment, []threads.ReviewThread, error) {
				convo, refreshedThreads, err := service.FetchData(ctx, ghContext, true, force)
				if err != nil {
					return threads.PullRequestInfo{}, nil, nil, err
				}
				info, err := service.FetchPullRequestInfo(ctx, ghContext)
				if err != nil {
					return threads.PullRequestInfo{}, nil, nil, err
				}
				return info, convo, refreshedThreads, nil
			},
		}
		// The TUI owns the screen from here on; stray warnings would corrupt it.
		service.SetLogWriter(io.Discard)
		return interactive.Run(cfg)
	}

	authorFilter := strings.TrimSpace(*authorFlag)
	textFilter := strings.TrimSpace(*textFlag)
	conversationComments = threads.FilterConversationComments(conversationComments, authorFilter, textFilter)
	reviewThreads = threads.FilterReviewThreads(reviewThreads, authorFilter, status, textFilter)

	payload := threads.BuildPayload(ghContext, conversationComments, reviewThreads)

	switch format {
	case outputJSON:
		body, err := render.DumpJSON(payload, colourEnabled)
		if err != nil {
			return err
		}
		fmt.Fprintln(a.out, body)
	case outputSummary:
		options := render.Options{
			Colour:   colourEnabled,
			ShowDiff: showDiff,
			Markdown: markdownEnabled,
			Width:    terminalWidth(a.outFile),
		}
		render.PrintSummary(a.out, payload, options)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	return nil
}

func (a *App) detectPullRequestFromBranch(ctx context.Context, owner, repo string) (int, error) {
	branch, err := gitlocal.CurrentBranch(ctx)
	if err != nil || branch == "" {
		return 0, errors.New("failed to determine current git branch")
	}
	prs, err := a.client.ListOpenPullRequestsByHead(ctx, owner, repo, branch)
	if err != nil {
		return 0, fmt.Errorf("failed to detect pull request for current branch: %w", err)
	}
	open := make([]int, 0, len(prs))
	for _, pr := range prs {
		if strings.EqualFold(pr.State, "OPEN") {
			open = append(open, pr.Number)
		}
	}
	if len(open) == 0 {
		return 0, errors.New("no open pull requests found for the current branch; specify one explicitly")
	}
	if len(open) > 1 {
		formatted := make([]string, len(open))
		for i, num := range open {
			formatted[i] = fmt.Sprintf("#%d", num)
		}
		return 0, fmt.Errorf("multiple open pull requests found for the current branch (%s); specify one explicitly", strings.Join(formatted, ", "))
	}
	return open[0], nil
}

func splitRepoSlug(slug string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(slug), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repository format: %s", slug)
	}
	return parts[0], parts[1], nil
}

func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func terminalWidth(file *os.File) int {
	if file == nil {
		return 80
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width <= 0 {
		return 80
	}
	return width
}

type outputFormat string

const (
	outputJSON    outputFormat = "json"
	outputSummary outputFormat = "summary"
)

func parseOutputFormat(value string) (outputFormat, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "json":
		return outputJSON, nil
	case "summary", "":
		return outputSummary, nil
	default:
		return "", fmt.Errorf("unsupported format: %s", value)
	}
}

func parseStatusFilter(value string) (threads.StatusFilter, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return threads.StatusAll, nil
	case "resolved":
		return threads.StatusResolved, nil
	case "unresolved":
		return threads.StatusUnresolved, nil
	default:
		return "", fmt.Errorf("unsupported status filter: %s", value)
	}
}
