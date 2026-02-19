package database

import (
	"database/sql"
	"fmt"
	"time"
)

// BeadComment represents a comment on a bead
type BeadComment struct {
	ID             string
	BeadID         string
	ParentID       string
	AuthorID       string
	AuthorUsername string
	Content        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Edited         bool
	Deleted        bool
}

// CommentMention represents a user mention in a comment
type CommentMention struct {
	ID                string
	CommentID         string
	MentionedUserID   string
	MentionedUsername string
	NotifiedAt        *time.Time
	CreatedAt         time.Time
}

// CreateComment inserts a new comment
func (d *Database) CreateComment(comment *BeadComment) error {
	query := `
		INSERT INTO bead_comments (
			id, bead_id, parent_id, author_id, author_username,
			content, created_at, updated_at, edited, deleted
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := d.db.Exec(rebind(query),
		comment.ID,
		comment.BeadID,
		sqlNullString(comment.ParentID),
		comment.AuthorID,
		comment.AuthorUsername,
		comment.Content,
		comment.CreatedAt,
		comment.UpdatedAt,
		comment.Edited,
		comment.Deleted,
	)

	if err != nil {
		return fmt.Errorf("failed to create comment: %w", err)
	}
	return nil
}

// GetCommentsByBeadID retrieves all comments for a bead
func (d *Database) GetCommentsByBeadID(beadID string) ([]*BeadComment, error) {
	query := `
		SELECT id, bead_id, parent_id, author_id, author_username,
			   content, created_at, updated_at, edited, deleted
		FROM bead_comments
		WHERE bead_id = ? AND deleted = 0
		ORDER BY created_at ASC
	`

	rows, err := d.db.Query(rebind(query), beadID)
	if err != nil {
		return nil, fmt.Errorf("failed to get comments: %w", err)
	}
	defer rows.Close()

	var comments []*BeadComment
	for rows.Next() {
		comment := &BeadComment{}
		var parentID sql.NullString

		err := rows.Scan(
			&comment.ID,
			&comment.BeadID,
			&parentID,
			&comment.AuthorID,
			&comment.AuthorUsername,
			&comment.Content,
			&comment.CreatedAt,
			&comment.UpdatedAt,
			&comment.Edited,
			&comment.Deleted,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan comment: %w", err)
		}

		comment.ParentID = parentID.String
		comments = append(comments, comment)
	}

	return comments, nil
}

// GetComment retrieves a single comment by ID
func (d *Database) GetComment(commentID string) (*BeadComment, error) {
	query := `
		SELECT id, bead_id, parent_id, author_id, author_username,
			   content, created_at, updated_at, edited, deleted
		FROM bead_comments
		WHERE id = ?
	`

	comment := &BeadComment{}
	var parentID sql.NullString

	err := d.db.QueryRow(rebind(query), commentID).Scan(
		&comment.ID,
		&comment.BeadID,
		&parentID,
		&comment.AuthorID,
		&comment.AuthorUsername,
		&comment.Content,
		&comment.CreatedAt,
		&comment.UpdatedAt,
		&comment.Edited,
		&comment.Deleted,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("comment not found: %s", commentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get comment: %w", err)
	}

	comment.ParentID = parentID.String
	return comment, nil
}

// UpdateComment updates a comment's content
func (d *Database) UpdateComment(commentID, content string) error {
	query := `
		UPDATE bead_comments
		SET content = ?, updated_at = ?, edited = 1
		WHERE id = ? AND deleted = 0
	`

	result, err := d.db.Exec(rebind(query), content, time.Now(), commentID)
	if err != nil {
		return fmt.Errorf("failed to update comment: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("comment not found or already deleted: %s", commentID)
	}

	return nil
}

// DeleteComment soft deletes a comment
func (d *Database) DeleteComment(commentID string) error {
	query := `
		UPDATE bead_comments
		SET deleted = 1, updated_at = ?
		WHERE id = ?
	`

	result, err := d.db.Exec(rebind(query), time.Now(), commentID)
	if err != nil {
		return fmt.Errorf("failed to delete comment: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("comment not found: %s", commentID)
	}

	return nil
}

// CreateMention inserts a new mention
func (d *Database) CreateMention(mention *CommentMention) error {
	query := `
		INSERT INTO comment_mentions (
			id, comment_id, mentioned_user_id, mentioned_username,
			notified_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := d.db.Exec(rebind(query),
		mention.ID,
		mention.CommentID,
		mention.MentionedUserID,
		mention.MentionedUsername,
		sqlNullTime(mention.NotifiedAt),
		mention.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create mention: %w", err)
	}
	return nil
}

// GetMentionsByComment retrieves all mentions in a comment
func (d *Database) GetMentionsByComment(commentID string) ([]*CommentMention, error) {
	query := `
		SELECT id, comment_id, mentioned_user_id, mentioned_username,
			   notified_at, created_at
		FROM comment_mentions
		WHERE comment_id = ?
	`

	rows, err := d.db.Query(rebind(query), commentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get mentions: %w", err)
	}
	defer rows.Close()

	var mentions []*CommentMention
	for rows.Next() {
		mention := &CommentMention{}
		var notifiedAt sql.NullTime

		err := rows.Scan(
			&mention.ID,
			&mention.CommentID,
			&mention.MentionedUserID,
			&mention.MentionedUsername,
			&notifiedAt,
			&mention.CreatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan mention: %w", err)
		}

		if notifiedAt.Valid {
			mention.NotifiedAt = &notifiedAt.Time
		}

		mentions = append(mentions, mention)
	}

	return mentions, nil
}

// MarkMentionNotified marks a mention as notified
func (d *Database) MarkMentionNotified(mentionID string) error {
	query := `
		UPDATE comment_mentions
		SET notified_at = ?
		WHERE id = ? AND notified_at IS NULL
	`

	_, err := d.db.Exec(rebind(query), time.Now(), mentionID)
	if err != nil {
		return fmt.Errorf("failed to mark mention as notified: %w", err)
	}
	return nil
}
