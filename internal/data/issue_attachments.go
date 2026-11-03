package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

// MaxAttachmentsPerIssue mirrors the client's MAX_PHOTOS and the limit the UI
// explains when it is reached. Six is two rows of tiles on a 360 px phone and
// one row on the desktop modal; more than that is a filing cabinet, not
// evidence, and it is the point where the per-issue payload stops being
// obviously small.
const MaxAttachmentsPerIssue = 6

// ErrTooManyAttachments: the issue already has MaxAttachmentsPerIssue photos.
// Kept separate from a validation error because the handler answers 409 rather
// than 422 -- the request is well formed, the issue is full.
var ErrTooManyAttachments = errors.New("issue already has the maximum number of photos")

// IssueAttachment is one photo on an issue: its metadata only. The bytes are
// deliberately absent and are fetched one at a time by the handler that serves
// them; an Issue is serialised into list responses, and a photo must never be
// able to ride along into one.
type IssueAttachment struct {
	ID        int64     `json:"id"`
	IssueID   int64     `json:"issue_id"`
	CreatedAt time.Time `json:"created_at"`

	// Nullable: deleting an account keeps the photo and loses only the name of
	// who took it. Accounts are normally deactivated rather than deleted.
	CreatedBy     *int64 `json:"created_by"`
	CreatedByName string `json:"created_by_name,omitempty"`

	// Written from what the server decoded and re-encoded, never from the
	// upload's headers.
	MimeType string `json:"mime_type"`
	ByteSize int    `json:"byte_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`

	// Hex SHA-256 of the stored bytes, used as the ETag.
	SHA256 string `json:"sha256"`
}

// IssueAttachmentBlob is the one type that carries image bytes. It has no JSON
// tags and Data is explicitly excluded, so a future accidental writeJSON of it
// cannot base64 a photo into a response.
type IssueAttachmentBlob struct {
	ID        int64
	IssueID   int64
	CreatedAt time.Time
	MimeType  string
	ByteSize  int
	Width     int
	Height    int
	SHA256    string
	Data      []byte `json:"-"`
}

type IssueAttachmentModel struct {
	DB *sql.DB
}

const issueAttachmentColumns = `a.id, a.issue_id, a.created_at, a.created_by,
	COALESCE(u.name, ''), a.mime_type, a.byte_size, a.width, a.height, a.sha256`

// Insert stores one already-normalised photo. The caller has decoded and
// re-encoded it; this method only writes the result.
//
// The issue row is locked first, so two uploads racing for the last slot on the
// same issue cannot both see five photos and both insert. A plain count-then-
// insert would allow exactly that, and the cap would be advisory.
func (m IssueAttachmentModel) Insert(att *IssueAttachment, data []byte) error {
	tx, err := m.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(`SELECT id FROM issues WHERE id = $1 FOR UPDATE`, att.IssueID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRecordNotFound
	}
	if err != nil {
		return err
	}

	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM issue_attachments WHERE issue_id = $1`, att.IssueID).Scan(&count); err != nil {
		return err
	}
	if count >= MaxAttachmentsPerIssue {
		return ErrTooManyAttachments
	}

	err = tx.QueryRow(`
		INSERT INTO issue_attachments
			(issue_id, created_by, mime_type, byte_size, width, height, sha256, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`,
		att.IssueID, att.CreatedBy, att.MimeType, len(data), att.Width, att.Height, att.SHA256, data,
	).Scan(&att.ID, &att.CreatedAt)
	if err != nil {
		return err
	}
	att.ByteSize = len(data)
	return tx.Commit()
}

// GetBlob reads one photo's bytes, scoped by both ids so a guessed attachment
// id cannot be fetched through a different issue. This is the only query in the
// package that names `data`.
func (m IssueAttachmentModel) GetBlob(issueID, attachmentID int64) (*IssueAttachmentBlob, error) {
	query := `SELECT a.id, a.issue_id, a.created_at, a.mime_type, a.byte_size,
			a.width, a.height, a.sha256, a.data
		FROM issue_attachments a
		WHERE a.issue_id = $1 AND a.id = $2`

	var b IssueAttachmentBlob
	err := m.DB.QueryRow(query, issueID, attachmentID).Scan(
		&b.ID, &b.IssueID, &b.CreatedAt, &b.MimeType, &b.ByteSize,
		&b.Width, &b.Height, &b.SHA256, &b.Data,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// Delete removes one photo, permanently. Scoped by both ids for the same reason
// as GetBlob. There is no soft delete: the manager was told it cannot be undone
// and retaining guest-room images while claiming otherwise would be worse than
// either choice made honestly.
func (m IssueAttachmentModel) Delete(issueID, attachmentID int64) error {
	res, err := m.DB.Exec(
		`DELETE FROM issue_attachments WHERE issue_id = $1 AND id = $2`,
		issueID, attachmentID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// loadIssueAttachments fills in the metadata of every photo on the given issues
// with one query, following loadIssueConsumables and friends. A JOIN would
// multiply the issue rows and break LIMIT and the total count, so this is a
// second query.
//
// It must never name a.data. Selecting a TOASTed column detoasts it, and doing
// that for every row of every issue list would turn a small JSON response into
// megabytes of image data.
func loadIssueAttachments(ctx context.Context, db *sql.DB, issues []*Issue) error {
	if len(issues) == 0 {
		return nil
	}

	byID := make(map[int64]*Issue, len(issues))
	ids := make([]int64, 0, len(issues))
	for _, is := range issues {
		is.Attachments = []*IssueAttachment{}
		byID[is.ID] = is
		ids = append(ids, is.ID)
	}

	query := `SELECT ` + issueAttachmentColumns + `
		FROM issue_attachments a
		LEFT JOIN users u ON u.id = a.created_by
		WHERE a.issue_id = ANY($1::bigint[])
		ORDER BY a.id`

	rows, err := db.QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var a IssueAttachment
		var createdBy sql.NullInt64
		if err := rows.Scan(
			&a.ID, &a.IssueID, &a.CreatedAt, &createdBy,
			&a.CreatedByName, &a.MimeType, &a.ByteSize,
			&a.Width, &a.Height, &a.SHA256,
		); err != nil {
			return err
		}
		if createdBy.Valid {
			id := createdBy.Int64
			a.CreatedBy = &id
		}
		if is, ok := byID[a.IssueID]; ok {
			is.Attachments = append(is.Attachments, &a)
		}
	}
	return rows.Err()
}
