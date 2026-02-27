package github

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number         int
	Title          string
	Body           string
	State          string
	URL            string
	Author         string
	HeadRef        string
	BaseRef        string
	Mergeable      string
	ReviewDecision string
	IsDraft        bool
	StatusChecks   []StatusCheck
}

// StatusCheck represents a single CI status check on a PR.
type StatusCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// CreateIssueRequest holds parameters for creating a GitHub issue.
type CreateIssueRequest struct {
	Title  string
	Body   string
	Labels []string
}

// CreatePRRequest holds parameters for creating a pull request.
type CreatePRRequest struct {
	Title string
	Body  string
	Base  string
	Head  string
	Draft bool
}

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	ID           int64
	Name         string // displayTitle (commit/PR title that triggered the run)
	WorkflowName string // workflow definition name, e.g. "CI" or "Build and Test"
	Status       string
	Conclusion   string
	URL          string
}

// RepoInfo represents basic information about a GitHub repository.
type RepoInfo struct {
	NameWithOwner string
	DefaultBranch string
	Description   string
	URL           string
	IsPrivate     bool
}

// Issue represents a GitHub issue.
type Issue struct {
	Number int
	Title  string
	Body   string
	State  string
	URL    string
	Author string
	Labels []string
}
