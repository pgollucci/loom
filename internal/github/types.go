package github

import "time"

// Issue represents a GitHub issue.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	Author    string    `json:"author"`
	Labels    []string  `json:"labels"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	Author    string    `json:"author"`
	HeadRef   string    `json:"headRefName"`
	BaseRef   string    `json:"baseRefName"`
	Mergeable string    `json:"mergeable"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	ID         int64     `json:"databaseId"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// RepoInfo holds basic information about the GitHub repository.
type RepoInfo struct {
	NameWithOwner string `json:"nameWithOwner"`
	DefaultBranch string `json:"defaultBranchRef"`
	Description   string `json:"description"`
	URL           string `json:"url"`
	IsPrivate     bool   `json:"isPrivate"`
}

// CreateIssueRequest holds parameters for creating an issue.
type CreateIssueRequest struct {
	Title  string
	Body   string
	Labels []string
}

// CreatePRRequest holds parameters for creating a pull request.
type CreatePRRequest struct {
	Title string
	Body  string
	Head  string // branch name
	Base  string // target branch (default: "main")
	Draft bool
}
