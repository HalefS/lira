package main

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// systemStatusHandler powers the System page: CPU and RAM usage, database
// latency and connection pool stats, and a few process-level numbers. It's
// manager-only (see routes.go) since it's operational/infra information
// rather than something technicians need day to day.
func (app *application) systemStatusHandler(w http.ResponseWriter, r *http.Request) {
	cpu := data.ReadCPU()
	mem := data.ReadMemory()

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	dbStart := time.Now()
	dbErr := app.models.Users.DB.PingContext(ctx)
	dbLatencyMs := time.Since(dbStart).Seconds() * 1000

	poolStats := app.models.Users.DB.Stats()

	database := envelope{
		"reachable":            dbErr == nil,
		"latency_ms":           round1ms(dbLatencyMs),
		"open_connections":     poolStats.OpenConnections,
		"in_use":               poolStats.InUse,
		"idle":                 poolStats.Idle,
		"max_open_connections": poolStats.MaxOpenConnections,
	}
	if dbErr != nil {
		database["error"] = dbErr.Error()
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"system": envelope{
			"timestamp":      time.Now(),
			"uptime_seconds": int(data.Uptime().Seconds()),
			"go_version":     runtime.Version(),
			"goroutines":     runtime.NumGoroutine(),
			"os":             runtime.GOOS,
			"cpu":            cpu,
			"memory":         mem,
			"database":       database,
		},
	}, nil)
}

func round1ms(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
