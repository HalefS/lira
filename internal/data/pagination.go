package data

import (
	"context"
	"database/sql"
	"time"
)

// Listings show a page of rows and load the rest on demand, rather than sending
// every row the table has ever held. Twenty is about two screens of a table on a
// laptop, which is enough to see the shape of the data and find what you are
// after before committing to a request.
const DefaultListLimit = 20

// MaxListLimit caps what one request may ask for. The cap exists so a stray or
// curious client cannot turn "load more" into "download the whole table" in a
// single call, and so the row count a caller can hold is bounded rather than
// being whatever the database happens to contain. A client that reaches it is
// told so by the handler, which is what stops it reading "1,000 of 4,000" as
// "that is all of them".
const MaxListLimit = 1000

// CountMatching totals the rows a listing query would return, given the same
// FROM/WHERE the row query used.
//
// The caller passes the identical string it built for its own SELECT rather than
// having this rebuild the filters: that is the whole point. Two separately
// written WHERE clauses drift apart the moment a filter is added to one of them,
// and the symptom is a "Showing 20 of 8" that is quietly wrong. Sharing the
// string means the count cannot disagree with the rows.
func CountMatching(ctx context.Context, db *sql.DB, fromAndWhere string, args []any) (int, error) {
	var total int
	query := `SELECT COUNT(*) ` + fromAndWhere
	if err := db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// listContext is the timeout shared by the count and the row query of a single
// listing request. Both are fast, and they run back to back.
func listContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
