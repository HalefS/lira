package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/report"
	_ "github.com/lib/pq"
)

const version = "1.0.0"

type config struct {
	port int
	env  string
	db   struct {
		dsn          string
		maxOpenConns int
		maxIdleConns int
		maxIdleTime  time.Duration
	}
	limiter struct {
		rps     float64
		burst   int
		enabled bool
	}
	cors struct {
		trustedOrigins []string
	}
	report struct {
		// chromeBin optionally pins the Chromium-based browser used to print
		// PDF reports. Empty means "discover it", which searches PATH and the
		// usual install locations.
		chromeBin string
	}
}

type application struct {
	config  config
	logger  *slog.Logger
	models  data.Models
	browser *report.Browser
	// browserErr is why browser is nil, kept so the PDF endpoint can tell the
	// user whether no browser is installed or the one they pointed at is wrong.
	browserErr error
}

func main() {
	var cfg config

	flag.IntVar(&cfg.port, "port", 4000, "API server port")
	flag.StringVar(&cfg.env, "env", "development", "Environment (development|staging|production)")

	flag.StringVar(&cfg.db.dsn, "db-dsn", os.Getenv("LIRADB_DSN"), "PostgreSQL DSN")
	flag.IntVar(&cfg.db.maxOpenConns, "db-max-open-conns", 25, "PostgreSQL max open connections")
	flag.IntVar(&cfg.db.maxIdleConns, "db-max-idle-conns", 25, "PostgreSQL max idle connections")
	flag.DurationVar(&cfg.db.maxIdleTime, "db-max-idle-time", 15*time.Minute, "PostgreSQL max connection idle time")

	flag.Float64Var(&cfg.limiter.rps, "limiter-rps", 100, "Rate limiter max requests per second")
	flag.IntVar(&cfg.limiter.burst, "limiter-burst", 200, "Rate limiter max burst")
	flag.BoolVar(&cfg.limiter.enabled, "limiter-enabled", true, "Enable rate limiter")

	flag.Func("cors-trusted-origins", "Trusted CORS origins (space separated)", func(val string) error {
		cfg.cors.trustedOrigins = append(cfg.cors.trustedOrigins, val)
		return nil
	})

	flag.StringVar(&cfg.report.chromeBin, "report-chrome-bin", os.Getenv("LIRA_CHROME_BIN"),
		"Path to the Chromium-based browser used to print PDF reports (default: auto-detect)")

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	db, err := openDB(cfg)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("database connection pool established")

	app := &application{
		config: cfg,
		logger: logger,
		models: data.NewModels(db),
	}

	// Resolved once here so a misconfigured path is reported at boot rather
	// than on someone's first report download. Neither failure is fatal:
	// everything except the PDF endpoint works without a browser, and taking
	// the whole API down over a reporting feature would be worse.
	switch browser, err := report.NewBrowser(cfg.report.chromeBin); {
	case err == nil:
		app.browser = browser
		logger.Info("pdf report renderer ready", "browser", browser.Path())
	case errors.Is(err, report.ErrNoBrowser):
		app.browserErr = err
		logger.Warn("no chromium-based browser found: pdf reports are disabled",
			"hint", "install Chrome, Chromium or Edge, or set -report-chrome-bin / LIRA_CHROME_BIN")
	default:
		// An explicit -report-chrome-bin that does not resolve is an operator
		// mistake, so it is logged as an error rather than a warning.
		app.browserErr = err
		logger.Error("invalid pdf report browser: pdf reports are disabled", "error", err)
	}

	if err := app.seedDefaultManager(); err != nil {
		logger.Error("failed to seed default manager account", "error", err)
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.port),
		Handler:      app.routes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		ErrorLog:     slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	logger.Info("starting server", "addr", srv.Addr, "env", cfg.env)
	if err := srv.ListenAndServe(); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

func openDB(cfg config) (*sql.DB, error) {
	db, err := sql.Open("postgres", cfg.db.dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.db.maxOpenConns)
	db.SetMaxIdleConns(cfg.db.maxIdleConns)
	db.SetConnMaxIdleTime(cfg.db.maxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
