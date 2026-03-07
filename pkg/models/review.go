package models

import "time"

// ReviewType classifies the kind of review.
type ReviewType string

const (
	ReviewTypeCode    ReviewType = "code"
	ReviewTypeDesign  ReviewType = "design"
	ReviewTypeSprint  ReviewType = "sprint"
	ReviewTypeGeneral ReviewType = "general"
)

// ReviewStatus represents the lifecycle state of a review.
type ReviewStatus string

const (
	ReviewStatusPending   ReviewStatus = "pending"
	ReviewStatusInReview  ReviewStatus = "in_review"
	ReviewStatusApproved  ReviewStatus = "approved"
	ReviewStatusRejected  ReviewStatus = "rejected"
	ReviewStatusCancelled ReviewStatus = "cancelled"
)

// ReviewComment is a single comment on a review.
type ReviewComment struct {
	ID         string    `json:"id"`
	ReviewID   string    `json:"review_id,omitempty"`
	AuthorID   string    `json:"author_id"`
	Content    string    `json:"content"`
	Line       int       `json:"line,omitempty"`
	LineNumber int       `json:"line_number,omitempty"`
	FilePath   string    `json:"file_path,omitempty"`
	Resolved   bool      `json:"resolved"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ReviewFinding is a structured finding produced during review.
type ReviewFinding struct {
	ID          string    `json:"id"`
	ReviewID    string    `json:"review_id,omitempty"`
	Title       string    `json:"title,omitempty"`
	Severity    string    `json:"severity"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	Suggestion  string    `json:"suggestion,omitempty"`
	FilePath    string    `json:"file_path,omitempty"`
	Line        int       `json:"line,omitempty"`
	LineNumber  int       `json:"line_number,omitempty"`
	Resolved    bool      `json:"resolved"`
	Status      string    `json:"status,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Review represents a peer or automated review of a bead or artifact.
type Review struct {
	EntityMetadata
	ID          string          `json:"id"`
	BeadID      string          `json:"bead_id"`
	ProjectID   string          `json:"project_id"`
	Type        ReviewType      `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Status      ReviewStatus    `json:"status"`
	Verdict     string          `json:"verdict,omitempty"`
	ReviewerID  string          `json:"reviewer_id,omitempty"`
	AuthorID    string          `json:"author_id,omitempty"`
	Comments    []ReviewComment `json:"comments"`
	Findings    []ReviewFinding `json:"findings"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}
