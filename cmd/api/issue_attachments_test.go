package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/photo"
	"github.com/julienschmidt/httprouter"
	"github.com/lib/pq"
)

func withParam(r *http.Request, name, value string) *http.Request {
	params := httprouter.Params{{Key: name, Value: value}}
	return r.WithContext(context.WithValue(r.Context(), httprouter.ParamsKey, params))
}

func TestReadIDParamNamed(t *testing.T) {
	app := &application{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withParam(req, "attachment_id", "12")
	if id, err := app.readIDParamNamed(req, "attachment_id"); err != nil || id != 12 {
		t.Fatalf("valid id = %d, %v", id, err)
	}

	// The two ids in a nested path must not be interchangeable.
	req = withParam(httptest.NewRequest(http.MethodGet, "/", nil), "id", "7")
	if id, _ := app.readIDParamNamed(req, "id"); id != 7 {
		t.Fatalf("id param = %d, want 7", id)
	}
	req = withParam(httptest.NewRequest(http.MethodGet, "/", nil), "attachment_id", "12")
	if id, _ := app.readIDParamNamed(req, "attachment_id"); id != 12 {
		t.Fatalf("attachment_id param = %d, want 12", id)
	}

	for _, bad := range []string{"", "0", "-1", "abc", "1.5", " 12 ", "999999999999999999999"} {
		req := withParam(httptest.NewRequest(http.MethodGet, "/", nil), "attachment_id", bad)
		if _, err := app.readIDParamNamed(req, "attachment_id"); err == nil {
			t.Fatalf("readIDParamNamed(%q) did not fail", bad)
		}
	}

	// A missing param is an error, not a panic.
	if _, err := app.readIDParamNamed(httptest.NewRequest(http.MethodGet, "/", nil), "attachment_id"); err == nil {
		t.Fatal("missing param did not fail")
	}
}

func TestCanMutateIssue(t *testing.T) {
	issue := &data.Issue{LoggedBy: 5}

	tests := []struct {
		name string
		user *data.User
		want bool
	}{
		{"owner", &data.User{ID: 5, Role: "technician"}, true},
		{"manager not owner", &data.User{ID: 9, Role: "manager"}, true},
		{"unrelated technician", &data.User{ID: 9, Role: "technician"}, false},
	}
	for _, tt := range tests {
		if got := canMutateIssue(tt.user, issue); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestAttachmentHeaders(t *testing.T) {
	blob := &data.IssueAttachmentBlob{
		ID:       12,
		IssueID:  34,
		MimeType: "image/jpeg",
		SHA256:   strings.Repeat("ab", 32),
	}

	h, name := attachmentHeaders(blob)
	if got := h.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q", got)
	}
	if got, want := h.Get("ETag"), `"`+blob.SHA256+`"`; got != want {
		t.Errorf("ETag = %q, want %q", got, want)
	}
	if got := h.Get("Cache-Control"); !strings.Contains(got, "immutable") || !strings.Contains(got, "private") {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if name != "issue-34-photo-12.jpg" {
		t.Errorf("name = %q", name)
	}
	if got := h.Get("Content-Disposition"); !strings.Contains(got, "filename=issue-34-photo-12.jpg") {
		t.Errorf("Content-Disposition = %q", got)
	}
	if strings.ContainsAny(h.Get("Content-Disposition"), "\r\n") {
		t.Error("Content-Disposition contains a line break")
	}

	blob.MimeType = "image/png"
	blob.SHA256 = strings.Repeat("cd", 32)
	h, name = attachmentHeaders(blob)
	if name != "issue-34-photo-12.png" {
		t.Errorf("png name = %q", name)
	}
	if got := h.Get("Content-Type"); got != "image/png" {
		t.Errorf("png Content-Type = %q", got)
	}
}

func TestUploadErrorResponseStatuses(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"body too large", &http.MaxBytesError{Limit: 1}, http.StatusRequestEntityTooLarge},
		{"photo too large", photo.ErrTooLarge, http.StatusRequestEntityTooLarge},
		{"heic", photo.ErrHEIC, http.StatusUnsupportedMediaType},
		{"not an image", photo.ErrUnsupported, http.StatusUnsupportedMediaType},
		{"not multipart", errNotMultipart, http.StatusUnsupportedMediaType},
		{"corrupt image", photo.ErrCorrupt, http.StatusUnprocessableEntity},
		{"no photo part", errNoPhoto, http.StatusUnprocessableEntity},
		{"issue full", data.ErrTooManyAttachments, http.StatusConflict},
		{"issue gone", data.ErrRecordNotFound, http.StatusNotFound},
		{"issue deleted mid-upload", &pq.Error{Code: "23503"}, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &application{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/issues/1/attachments", nil)
			app.uploadErrorResponse(rec, req, tt.err)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// A part that is not called "photo" is skipped, and an empty body is a missing
// photo rather than a panic.
func TestReadPhotoPartRejectsAMissingPhoto(t *testing.T) {
	body := "--b\r\nContent-Disposition: form-data; name=\"caption\"\r\n\r\nhello\r\n--b--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", `multipart/form-data; boundary=b`)

	if _, err := readPhotoPart(req); !errors.Is(err, errNoPhoto) {
		t.Fatalf("error = %v, want errNoPhoto", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if _, err := readPhotoPart(req); !errors.Is(err, errNotMultipart) {
		t.Fatalf("error = %v, want errNotMultipart", err)
	}
}
