// Package github wraps the gh CLI to provide GitHub API access without
// additional dependencies. The gh binary handles OAuth token refresh,
// rate limiting, pagination, and outputs parseable JSON via --json flags.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Client wraps gh CLI commands for GitHub operations.
// workDir should be the project workspace so that gh can auto-detect the repo.
type Client struct {
	workDir string
	token   string // optional; if empty, gh uses its stored credentials
}

// NewClient creates a GitHub client rooted at workDir.
func NewClient(workDir, token string) *Client {
	return &Client{workDir: workDir, token: token}
}

// gh runs a gh CLI command and returns raw JSON output.
func (c *Client) gh(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = c.workDir
	if c.token != "" {
		cmd.Env = append(cmd.Environ(), "GH_TOKEN="+c.token)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return out, nil
}

// Environ returns the current environment without GH_TOKEN so we can add it.
func (c *Client) Environ() []string {
	cmd := exec.Command("env")
	cmd.Dir = c.workDir
	out, _ := cmd.Output()
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// DetectRepoURL returns the remote origin URL of the working directory.
func (c *Client) DetectRepoURL(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = c.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w\n%s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// GetRepoInfo returns basic information about the current repository.
func (c *Client) GetRepoInfo(ctx context.Context) (*RepoInfo, error) {
	type ghRepo struct {
		NameWithOwner    string `json:"nameWithOwner"`
		DefaultBranchRef struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
		Description string `json:"description"`
		URL         string `json:"url"`
		IsPrivate   bool   `json:"isPrivate"`
	}
	out, err := c.gh(ctx, "repo", "view", "--json",
		"nameWithOwner,defaultBranchRef,description,url,isPrivate")
	if err != nil {
		return nil, err
	}
	var r ghRepo
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("parse repo view: %w", err)
	}
	return &RepoInfo{
		NameWithOwner: r.NameWithOwner,
		DefaultBranch: r.DefaultBranchRef.Name,
		Description:   r.Description,
		URL:           r.URL,
		IsPrivate:     r.IsPrivate,
	}, nil
}

// ListIssues returns open issues. Pass state="closed" or "all" to change filter.
func (c *Client) ListIssues(ctx context.Context, state string) ([]Issue, error) {
	if state == "" {
		state = "open"
	}
	out, err := c.gh(ctx, "issue", "list",
		"--state", state, "--limit", "50",
		"--json", "number,title,body,state,url,author,labels,createdAt,updatedAt")
	if err != nil {
		return nil, err
	}
	type ghIssue struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		URL    string `json:"url"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}
	var raw []ghIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse issue list: %w", err)
	}
	issues := make([]Issue, 0, len(raw))
	for _, r := range raw {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		issues = append(issues, Issue{
			Number: r.Number,
			Title:  r.Title,
			Body:   r.Body,
			State:  r.State,
			URL:    r.URL,
			Author: r.Author.Login,
			Labels: labels,
		})
	}
	return issues, nil
}

// GetIssue returns a single issue by number.
func (c *Client) GetIssue(ctx context.Context, number int) (*Issue, error) {
	out, err := c.gh(ctx, "issue", "view", fmt.Sprintf("%d", number),
		"--json", "number,title,body,state,url,author,labels,createdAt,updatedAt")
	if err != nil {
		return nil, err
	}
	type ghIssue struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		URL    string `json:"url"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	var r ghIssue
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("parse issue view: %w", err)
	}
	labels := make([]string, 0, len(r.Labels))
	for _, l := range r.Labels {
		labels = append(labels, l.Name)
	}
	return &Issue{
		Number: r.Number,
		Title:  r.Title,
		Body:   r.Body,
		State:  r.State,
		URL:    r.URL,
		Author: r.Author.Login,
		Labels: labels,
	}, nil
}

// CreateIssue creates a new GitHub issue and returns it.
func (c *Client) CreateIssue(ctx context.Context, req CreateIssueRequest) (*Issue, error) {
	args := []string{"issue", "create", "--title", req.Title, "--body", req.Body}
	for _, l := range req.Labels {
		args = append(args, "--label", l)
	}
	// Add --json to get structured output
	args = append(args, "--json", "number,title,url")
	out, err := c.gh(ctx, args...)
	if err != nil {
		return nil, err
	}
	var r struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("parse create issue: %w", err)
	}
	return &Issue{Number: r.Number, Title: r.Title, URL: r.URL, State: "open"}, nil
}

// CommentOnIssue adds a comment to an issue.
func (c *Client) CommentOnIssue(ctx context.Context, number int, body string) error {
	_, err := c.gh(ctx, "issue", "comment", fmt.Sprintf("%d", number), "--body", body)
	return err
}

// CloseIssue closes an issue, optionally with a comment.
func (c *Client) CloseIssue(ctx context.Context, number int, comment string) error {
	if comment != "" {
		if err := c.CommentOnIssue(ctx, number, comment); err != nil {
			return err
		}
	}
	_, err := c.gh(ctx, "issue", "close", fmt.Sprintf("%d", number))
	return err
}

// ListPRs returns pull requests. state: "open", "closed", "merged", "all".
func (c *Client) ListPRs(ctx context.Context, state string) ([]PullRequest, error) {
	if state == "" {
		state = "open"
	}
	out, err := c.gh(ctx, "pr", "list",
		"--state", state, "--limit", "50",
		"--json", "number,title,body,state,url,author,headRefName,baseRefName,mergeable,reviewDecision,isDraft,statusCheckRollup,createdAt,updatedAt")
	if err != nil {
		return nil, err
	}
	type ghStatusCheck struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	type ghPR struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		URL    string `json:"url"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		HeadRefName       string          `json:"headRefName"`
		BaseRefName       string          `json:"baseRefName"`
		Mergeable         string          `json:"mergeable"`
		ReviewDecision    string          `json:"reviewDecision"`
		IsDraft           bool            `json:"isDraft"`
		StatusCheckRollup []ghStatusCheck `json:"statusCheckRollup"`
	}
	var raw []ghPR
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse pr list: %w", err)
	}
	prs := make([]PullRequest, 0, len(raw))
	for _, r := range raw {
		checks := make([]StatusCheck, len(r.StatusCheckRollup))
		for i, c := range r.StatusCheckRollup {
			checks[i] = StatusCheck(c)
		}
		prs = append(prs, PullRequest{
			Number:         r.Number,
			Title:          r.Title,
			Body:           r.Body,
			State:          r.State,
			URL:            r.URL,
			Author:         r.Author.Login,
			HeadRef:        r.HeadRefName,
			BaseRef:        r.BaseRefName,
			Mergeable:      r.Mergeable,
			ReviewDecision: r.ReviewDecision,
			IsDraft:        r.IsDraft,
			StatusChecks:   checks,
		})
	}
	return prs, nil
}

// CreatePR creates a pull request and returns it.
func (c *Client) CreatePR(ctx context.Context, req CreatePRRequest) (*PullRequest, error) {
	base := req.Base
	if base == "" {
		base = "main"
	}
	args := []string{"pr", "create",
		"--title", req.Title,
		"--body", req.Body,
		"--base", base,
	}
	if req.Head != "" {
		args = append(args, "--head", req.Head)
	}
	if req.Draft {
		args = append(args, "--draft")
	}
	args = append(args, "--json", "number,title,url")
	out, err := c.gh(ctx, args...)
	if err != nil {
		return nil, err
	}
	var r struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("parse create pr: %w", err)
	}
	return &PullRequest{Number: r.Number, Title: r.Title, URL: r.URL, State: "open"}, nil
}

// MergePR merges a pull request. method: "merge", "rebase", "squash".
func (c *Client) MergePR(ctx context.Context, number int, method string) error {
	if method == "" {
		method = "merge"
	}
	flag := "--" + method
	_, err := c.gh(ctx, "pr", "merge", fmt.Sprintf("%d", number), flag, "--auto")
	return err
}

// ListWorkflowRuns returns the last N runs for a workflow file (e.g. "ci.yml").
// Pass an empty workflow to list all runs.
func (c *Client) ListWorkflowRuns(ctx context.Context, workflow string) ([]WorkflowRun, error) {
	args := []string{"run", "list", "--limit", "10",
		"--json", "databaseId,displayTitle,workflowName,status,conclusion,url,createdAt,updatedAt"}
	if workflow != "" {
		args = append(args, "--workflow", workflow)
	}
	out, err := c.gh(ctx, args...)
	if err != nil {
		return nil, err
	}
	type ghRun struct {
		DatabaseID   int64  `json:"databaseId"`
		DisplayTitle string `json:"displayTitle"`
		WorkflowName string `json:"workflowName"`
		Status       string `json:"status"`
		Conclusion   string `json:"conclusion"`
		URL          string `json:"url"`
	}
	var raw []ghRun
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse run list: %w", err)
	}
	runs := make([]WorkflowRun, 0, len(raw))
	for _, r := range raw {
		runs = append(runs, WorkflowRun{
			ID:           r.DatabaseID,
			Name:         r.DisplayTitle,
			WorkflowName: r.WorkflowName,
			Status:       r.Status,
			Conclusion:   r.Conclusion,
			URL:          r.URL,
		})
	}
	return runs, nil
}

// ListFailedWorkflowRuns returns the last N failed runs for a workflow file (e.g. "ci.yml").
func (c *Client) ListFailedWorkflowRuns(ctx context.Context, workflow string) ([]WorkflowRun, error) {
	args := []string{"run", "list", "--limit", "10",
		"--json", "databaseId,displayTitle,workflowName,status,conclusion,url,createdAt,updatedAt",
		"--status", "failure"}
	if workflow != "" {
		args = append(args, "--workflow", workflow)
	}
	out, err := c.gh(ctx, args...)
	if err != nil {
		return nil, err
	}
	type ghRun struct {
		DatabaseID   int64  `json:"databaseId"`
		DisplayTitle string `json:"displayTitle"`
		WorkflowName string `json:"workflowName"`
		Status       string `json:"status"`
		Conclusion   string `json:"conclusion"`
		URL          string `json:"url"`
	}
	var raw []ghRun
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse run list: %w", err)
	}
	runs := make([]WorkflowRun, 0, len(raw))
	for _, r := range raw {
		runs = append(runs, WorkflowRun{
			ID:           r.DatabaseID,
			Name:         r.DisplayTitle,
			WorkflowName: r.WorkflowName,
			Status:       r.Status,
			Conclusion:   r.Conclusion,
			URL:          r.URL,
		})
	}
	return runs, nil
}
