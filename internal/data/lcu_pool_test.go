package data

import (
	"os"
	"strings"
	"testing"
)

// There are no database-backed tests in this package -- internal/data/lcu_test.go
// covers pure functions only, and go.mod has no sqlmock or container runtime. These
// three are therefore SOURCE assertions, and they are honest about it: they cannot run
// a sweep, so they cannot prove a credit is exactly-once. What they can do is fail when
// the three properties below are edited away, each of which reads like a tidy-up and
// none of which is one.
//
// The live behaviour IS verified end to end against a real Postgres: sweeping eight
// historical trials credited six readers, and a second sweep credited nothing.

func lcuSource(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	// Comments are stripped so a paragraph ABOUT the invariant cannot satisfy the
	// assertion for the invariant. Line comments only, which is what this file uses.
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if i := strings.Index(l, "//"); i >= 0 {
			l = l[:i]
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// EXACTLY ONCE. The credit must be an INSERT that can absorb a duplicate, or two
// sweeps -- or one sweep racing another manager's -- put the same reader in twice.
//
// The UNIQUE index on lower(trim(serial)) is half of it and this is the other half:
// without the ON CONFLICT clause the duplicate raises instead of being absorbed, and
// the request 500s having already marked the trial passed. Both halves are load-
// bearing, which is why the test names the clause and not the table.
func TestResolveDueCreditsAbsorbingADuplicate(t *testing.T) {
	src := lcuSource(t, "lcu.go")
	start := strings.Index(src, "func (m LCUModel) ResolveDue(")
	if start < 0 {
		t.Fatal("ResolveDue not found")
	}
	body := src[start:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		body = body[:end]
	}

	if !strings.Contains(body, "ON CONFLICT (lower(trim(serial))) DO NOTHING") {
		t.Error("ResolveDue does not insert with ON CONFLICT (lower(trim(serial))) DO NOTHING; " +
			"without it a duplicate credit raises instead of being absorbed, and the sweep " +
			"500s having already marked the trial passed")
	}
	// Only a PASSED unit may be credited. The unit table also carries 'failed', and a
	// failed reader in the pool is a scrapped reader counted as spare.
	if !strings.Contains(body, "r.status = $3") {
		t.Error("ResolveDue does not restrict the credit to passed units; check the " +
			"WHERE on the credited CTE")
	}
	// One statement, so the UPDATE and the INSERT commit or abort together. There must
	// be no window in which a reader is passed but uncounted.
	if strings.Contains(body, "BeginTx") {
		t.Error("ResolveDue opens a transaction; the credit is meant to be ONE statement " +
			"(a data-modifying CTE is already atomic), and a second statement is how a " +
			"passed-but-uncounted reader happens")
	}
}

// THE ACTIVE LIST MUST NOT DUPLICATE THE RESTING SECTION. ListAll and ListForUser used
// to return resolved units as well, which was harmless when the resting section did not
// exist and is the same reader twice on one page now that it does -- once as live work
// carrying a "record today's result" button that does nothing to it.
func TestActiveListFiltersToActiveOnly(t *testing.T) {
	src := lcuSource(t, "lcu.go")
	for _, fn := range []string{"func (m LCUModel) ListAll(", "func (m LCUModel) ListForUser("} {
		start := strings.Index(src, fn)
		if start < 0 {
			t.Fatalf("%s not found", fn)
		}
		body := src[start:]
		if end := strings.Index(body, "}, limit)"); end >= 0 {
			body = body[:end]
		}
		if !strings.Contains(body, "u.status = $") {
			t.Errorf("%s does not filter on u.status; resolved readers will appear in the "+
				"active list AND in the resting section", fn)
		}
	}
}

// THE POOL IS A STOCK NUMBER, AND A ROW IS NOT A READER LEAVING THE BUILDING.
// Deleting a trial is tidying; it must not silently shrink the pool.
func TestDeleteRefusesAReaderInThePool(t *testing.T) {
	src := lcuSource(t, "lcu.go")
	start := strings.Index(src, "func (m LCUModel) Delete(")
	if start < 0 {
		t.Fatal("Delete not found")
	}
	body := src[start:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "ErrUnitInReconditionedPool") {
		t.Error("Delete does not check ErrUnitInReconditionedPool; deleting a trial row " +
			"would take a reader out of the pool count without a reader leaving the cupboard")
	}
	// Matched on the NORMALISED serial, not on unit_id: the ledger deliberately keeps a
	// reader whose trial row is gone, and a re-trialled reader has a new unit_id.
	if !strings.Contains(body, "lower(trim(u.serial)) = lower(trim(rc.serial))") {
		t.Error("Delete's pool check does not match on the normalised serial; a reader " +
			"re-trialled under a new unit_id would dodge its own guard")
	}
}
