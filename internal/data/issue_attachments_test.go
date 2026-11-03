package data

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

// There are no database-backed tests in this package. These are source
// assertions, in the same spirit as lcu_pool_test.go: they cannot run a query,
// but they fail when one of the two rules that keep photo bytes out of every
// issue list is quietly edited away.

// The metadata loader must never name a.data. Selecting a TOASTed column
// detoasts it, and GetAll would then read every photo on every listed issue.
func TestLoadIssueAttachmentsNeverSelectsData(t *testing.T) {
	src := lcuSource(t, "issue_attachments.go")

	start := strings.Index(src, "func loadIssueAttachments(")
	if start < 0 {
		t.Fatal("loadIssueAttachments not found")
	}
	body := src[start:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "a.data") {
		t.Error("loadIssueAttachments names a.data; selecting a TOASTed column " +
			"detoasts it, so every issue list would read every photo")
	}
	if !strings.Contains(body, "issueAttachmentColumns") {
		t.Error("loadIssueAttachments does not use the shared column list")
	}
}

// Image bytes must not be able to ride into a JSON response through the struct
// that an Issue carries. Only IssueAttachmentBlob has Data, and it is json:"-".
func TestIssueAttachmentCarriesNoBytes(t *testing.T) {
	typ := reflect.TypeOf(IssueAttachment{})
	if _, ok := typ.FieldByName("Data"); ok {
		t.Fatal("IssueAttachment has a Data field; an Issue is serialised into list responses")
	}

	issue := reflect.TypeOf(Issue{})
	f, ok := issue.FieldByName("Attachments")
	if !ok {
		t.Fatal("Issue has no Attachments field")
	}
	if f.Type != reflect.TypeOf([]*IssueAttachment{}) {
		t.Fatalf("Issue.Attachments is %v", f.Type)
	}

	blob, ok := reflect.TypeOf(IssueAttachmentBlob{}).FieldByName("Data")
	if !ok {
		t.Fatal("IssueAttachmentBlob has no Data field")
	}
	if blob.Tag.Get("json") != "-" {
		t.Fatalf("IssueAttachmentBlob.Data json tag = %q, want -", blob.Tag.Get("json"))
	}
}

// The migration is not covered by the loader tests, and it carries two
// properties that are easy to lose in a rewrite: photos disappear with their
// issue, and the declared size is the real size.
func TestIssueAttachmentsMigration(t *testing.T) {
	b, err := os.ReadFile("../../migrations/000037_create_issue_attachments.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("migration starts with a UTF-8 BOM; psql rejects it and the lint does not catch it")
	}
	if !bytes.HasPrefix(b, []byte("--")) {
		t.Fatal("migration does not start with a -- comment")
	}
	src := string(b)
	for _, want := range []string{
		"ON DELETE CASCADE",
		"octet_length(data) = byte_size",
		"SET STORAGE EXTERNAL",
		"issue_attachments (issue_id, id)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("migration is missing %q", want)
		}
	}

	down, err := os.ReadFile("../../migrations/000037_create_issue_attachments.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	if !strings.Contains(string(down), "DROP TABLE IF EXISTS issue_attachments") {
		t.Error("down migration does not drop the table")
	}
}
