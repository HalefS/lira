package data

import (
	"context"
	"database/sql"
	"time"
)

// DatabaseStat is one database on the PostgreSQL instance, as PostgreSQL's own
// statistics collector sees it. These are instance-wide counters, not numbers
// derived from LIRA's tables, so they also cover the other databases sharing
// the server.
type DatabaseStat struct {
	Name              string     `json:"name"`
	Current           bool       `json:"current"`
	AllowConnections  bool       `json:"allow_connections"`
	SizeBytes         int64      `json:"size_bytes"`
	ActiveConnections int64      `json:"active_connections"`
	Transactions      int64      `json:"transactions"`
	BlocksHit         int64      `json:"blocks_hit"`
	BlocksRead        int64      `json:"blocks_read"`
	CacheHitPercent   float64    `json:"cache_hit_percent"`
	Deadlocks         int64      `json:"deadlocks"`
	TempFiles         int64      `json:"temp_files"`
	StatsReset        *time.Time `json:"stats_reset"`
}

// PostgresStats is the instance-level context above the per-database table.
type PostgresStats struct {
	Version   string         `json:"version"`
	StartedAt *time.Time     `json:"started_at"`
	Databases []DatabaseStat `json:"databases"`
}

// ReadPostgresStats collects statistics for every database on the instance.
// It runs on the pool the application already owns: no second connection and
// no superuser rights are needed, just read access to the standard statistics
// views (which PostgreSQL grants to public).
//
// A failure here must not take the rest of the System page down, so the caller
// treats the error as optional information rather than an HTTP error.
func ReadPostgresStats(ctx context.Context, db *sql.DB) (*PostgresStats, error) {
	stats := &PostgresStats{Databases: []DatabaseStat{}}

	var started sql.NullTime
	if err := db.QueryRowContext(ctx,
		`SELECT current_setting('server_version'), pg_postmaster_start_time()`,
	).Scan(&stats.Version, &started); err != nil {
		return nil, err
	}
	if started.Valid {
		stats.StartedAt = &started.Time
	}

	// pg_stat_database has a row per database. The LEFT JOIN keeps template and
	// freshly created databases in the list even before they have collected any
	// activity, and COALESCE turns their absent counters into honest zeroes.
	// A database with no reads yet gets a 0% cache hit, which the UI hides.
	rows, err := db.QueryContext(ctx, `
		SELECT d.datname,
		       d.datallowconn,
		       d.datname = current_database(),
		       COALESCE(pg_database_size(d.oid), 0),
		       COALESCE(s.numbackends, 0),
		       COALESCE(s.xact_commit, 0) + COALESCE(s.xact_rollback, 0),
		       COALESCE(s.blks_hit, 0),
		       COALESCE(s.blks_read, 0),
		       COALESCE(s.deadlocks, 0),
		       COALESCE(s.temp_files, 0),
		       s.stats_reset
		FROM pg_database d
		LEFT JOIN pg_stat_database s ON s.datid = d.oid
		ORDER BY (d.datname = current_database()) DESC, d.datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var row DatabaseStat
		var reset sql.NullTime
		if err := rows.Scan(
			&row.Name, &row.AllowConnections, &row.Current, &row.SizeBytes,
			&row.ActiveConnections, &row.Transactions, &row.BlocksHit,
			&row.BlocksRead, &row.Deadlocks, &row.TempFiles, &reset,
		); err != nil {
			return nil, err
		}
		row.CacheHitPercent = cacheHitPercent(row.BlocksHit, row.BlocksRead)
		if reset.Valid {
			row.StatsReset = &reset.Time
		}
		stats.Databases = append(stats.Databases, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return stats, nil
}

// cacheHitPercent is the share of block reads served from PostgreSQL's shared
// buffers instead of disk. A fresh instance has no reads at all, and reporting
// that as 0% would look like a failure, so the UI checks the raw counters.
func cacheHitPercent(hits, reads int64) float64 {
	total := hits + reads
	if total <= 0 {
		return 0
	}
	return round1(float64(hits) / float64(total) * 100)
}
