package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/photo"
	"github.com/lib/pq"
)

// maxUploadBody is the largest multipart body this app accepts. It is one
// photo plus generous room for the boundary and headers; it applies to the
// attachment route only, so readJSON's 1 MiB cap on every other endpoint is
// untouched.
const maxUploadBody = photo.MaxBytes + (1 << 20)

var (
	errNotMultipart = errors.New("request is not multipart/form-data")
	errNoPhoto      = errors.New("multipart body has no photo part")
)

// canMutateIssue is the owner-or-manager rule shared by editing an issue,
// deleting it, and adding a photo to it. One function so the three cannot
// drift apart.
func canMutateIssue(user *data.User, issue *data.Issue) bool {
	return user.Role == "manager" || issue.LoggedBy == user.ID
}

// createIssueAttachmentHandler accepts exactly one photo per request, as
// multipart/form-data with a single "photo" part.
//
// One photo at a time is deliberate: the client shrinks and uploads them one by
// one, so a progress line can name the photo that is in flight, a single bad
// photo does not fail the rest, and the server never holds six decoded images
// at once. It also keeps the bytes out of the issue's JSON body, which
// readJSON caps at 1 MiB and decodes with DisallowUnknownFields.
func (app *application) createIssueAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	issueID, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	issue, err := app.models.Issues.Get(issueID)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	user := app.contextGetUser(r)
	if !canMutateIssue(user, issue) {
		app.notPermittedResponse(w, r)
		return
	}

	// The cap is applied before the multipart reader sees a byte, so an
	// oversized body is refused while it is being read rather than after it is
	// in memory.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBody)

	src, err := readPhotoPart(r)
	if err != nil {
		app.uploadErrorResponse(w, r, err)
		return
	}

	res, err := photo.Normalise(src)
	if err != nil {
		app.uploadErrorResponse(w, r, err)
		return
	}

	att := &data.IssueAttachment{
		IssueID:       issueID,
		CreatedBy:     &user.ID,
		CreatedByName: user.Name,
		MimeType:      res.MimeType,
		Width:         res.Width,
		Height:        res.Height,
		SHA256:        hex.EncodeToString(res.SHA256[:]),
	}

	if err := app.models.IssueAttachments.Insert(att, res.Data); err != nil {
		app.uploadErrorResponse(w, r, err)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v1/issues/%d/attachments/%d", issueID, att.ID))
	app.writeJSON(w, http.StatusCreated, envelope{"attachment": att}, nil)
}

// showIssueAttachmentHandler serves one photo's bytes. Any authenticated user
// may read them, exactly as any authenticated user may read the issue those
// bytes document; gating the photo more tightly than its problem text would be
// security theatre. If guest-room photos ever need to be manager-only, the fix
// belongs in the issue read path, not here.
func (app *application) showIssueAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	issueID, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}
	attachmentID, err := app.readIDParamNamed(r, "attachment_id")
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	blob, err := app.models.IssueAttachments.GetBlob(issueID, attachmentID)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	headers, name := attachmentHeaders(blob)
	for key, values := range headers {
		w.Header()[key] = values
	}
	// ServeContent gives If-None-Match, Range and Last-Modified for free. The
	// ETag is the SHA-256 of the bytes, and an attachment never changes, so the
	// browser can keep it forever.
	http.ServeContent(w, r, name, blob.CreatedAt, bytes.NewReader(blob.Data))
}

// deleteIssueAttachmentHandler removes one photo permanently. Manager-only, as
// a deliberate asymmetry with upload: a technician who attached the wrong photo
// has to ask a manager, and the only record of the deletion is the log line
// below, because the row -- bytes included -- is gone.
func (app *application) deleteIssueAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	issueID, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}
	attachmentID, err := app.readIDParamNamed(r, "attachment_id")
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.IssueAttachments.Delete(issueID, attachmentID); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	user := app.contextGetUser(r)
	app.logger.Info("issue attachment deleted",
		"attachment_id", attachmentID,
		"issue_id", issueID,
		"user_id", user.ID,
	)
	app.writeJSON(w, http.StatusOK, envelope{"message": "attachment successfully deleted"}, nil)
}

// readPhotoPart streams the "photo" part into memory under an explicit limit.
// The part's declared Content-Type and filename are ignored: the bytes are the
// only thing trusted, and photo.Normalise sniffs them.
//
// This uses MultipartReader rather than ParseMultipartForm so nothing is ever
// spilled to a temporary file. The deployment promises the bytes live in
// Postgres, and a temp file would make that promise untrue for the duration of
// every upload.
func readPhotoPart(r *http.Request) ([]byte, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, errNotMultipart
	}

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, errNoPhoto
		}
		if err != nil {
			return nil, err
		}
		if part.FormName() != "photo" {
			part.Close()
			continue
		}

		data, err := io.ReadAll(io.LimitReader(part, photo.MaxBytes+1))
		part.Close()
		if err != nil {
			return nil, err
		}
		if len(data) > photo.MaxBytes {
			return nil, photo.ErrTooLarge
		}
		return data, nil
	}
}

// attachmentHeaders builds everything the response needs before the bytes are
// written. It is a pure function so the header rules -- stored MIME type, a
// quoted hash ETag, no sniffing, a filename that cannot inject a header -- are
// testable without a database.
func attachmentHeaders(att *data.IssueAttachmentBlob) (http.Header, string) {
	h := make(http.Header)
	h.Set("Content-Type", att.MimeType)
	h.Set("ETag", `"`+att.SHA256+`"`)
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")

	// The download name is derived, never stored: the client's filename could
	// be anything, and a header written with it would be an injection surface.
	name := fmt.Sprintf("issue-%d-photo-%d.jpg", att.IssueID, att.ID)
	if att.MimeType == "image/png" {
		name = fmt.Sprintf("issue-%d-photo-%d.png", att.IssueID, att.ID)
	}
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	return h, name
}

// uploadErrorResponse maps the upload-specific failures onto the status the
// client acts on: 413 means "do not retry, the file is too big", 415 means "this
// is not an image", 422 means "this claimed to be an image but would not
// decode". A deleted issue during an upload is a 404, not a 500.
func (app *application) uploadErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	var maxErr *http.MaxBytesError
	var pqErr *pq.Error

	switch {
	case errors.As(err, &maxErr), errors.Is(err, photo.ErrTooLarge):
		app.payloadTooLargeResponse(w, r)
	case errors.Is(err, photo.ErrHEIC):
		app.unsupportedMediaTypeResponse(w, r, "HEIC photos are not supported; upload a JPEG or PNG")
	case errors.Is(err, photo.ErrUnsupported):
		app.unsupportedMediaTypeResponse(w, r, "only JPEG and PNG images are supported")
	case errors.Is(err, errNotMultipart):
		app.unsupportedMediaTypeResponse(w, r, "the request must be multipart/form-data")
	case errors.Is(err, photo.ErrCorrupt):
		app.failedValidationResponse(w, r, map[string]string{"photo": "the image could not be read"})
	case errors.Is(err, errNoPhoto):
		app.failedValidationResponse(w, r, map[string]string{"photo": "a photo file is required"})
	case errors.Is(err, data.ErrTooManyAttachments):
		app.errorResponse(w, r, http.StatusConflict, err.Error())
	case errors.Is(err, data.ErrRecordNotFound):
		app.notFoundResponse(w, r)
	case errors.As(err, &pqErr) && pqErr.Code == "23503":
		// foreign_key_violation: the issue was deleted between the ownership
		// check and the insert.
		app.notFoundResponse(w, r)
	default:
		app.serverErrorResponse(w, r, err)
	}
}
