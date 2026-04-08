package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/logx"
	"github.com/tmater/wacht/internal/monitoring"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/server"
	"github.com/tmater/wacht/internal/store"
)

func main() {
	logger := logx.Configure("wacht-server")
	configPath := flag.String("config", "server.yaml", "path to server config file")
	dsn := flag.String("dsn", "", "Postgres DSN (e.g. postgres://user:pass@host/db)")
	flag.Parse()

	logger.Info("server starting", "config_path", *configPath)

	fatal := func(msg string, args ...any) {
		logger.Error(msg, args...)
		os.Exit(1)
	}

	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		fatal("load config failed", "config_path", *configPath, "err", err)
	}

	if *dsn == "" {
		fatal("missing required flag", "flag", "dsn")
	}

	db, err := store.New(*dsn)
	if err != nil {
		fatal("open database failed", "err", err)
	}
	defer db.Close()

	probes := make([]store.ProbeSeed, len(cfg.Probes))
	for i, probe := range cfg.Probes {
		probes[i] = store.ProbeSeed{ProbeID: probe.ID, Secret: probe.Secret}
	}
	if err := db.SeedProbes(probes); err != nil {
		fatal("seed probe credentials failed", "err", err)
	}
	if len(probes) == 0 {
		logger.Warn("no probes configured; probe authentication will reject all requests")
	}

	var seedUserID int64
	if cfg.SeedUser.Email != "" && cfg.SeedUser.Password != "" {
		exists, err := db.UserExists()
		if err != nil {
			fatal("check existing users failed", "err", err)
		}
		if !exists {
			u, err := db.CreateAdminUser(cfg.SeedUser.Email, cfg.SeedUser.Password)
			if err != nil {
				fatal("seed user failed", "err", err)
			}
			logger.Info("seeded dev user", "email_hash", logx.EmailHash(cfg.SeedUser.Email))
			seedUserID = u.ID
		} else {
			u, err := db.AuthenticateUser(cfg.SeedUser.Email, cfg.SeedUser.Password)
			if err != nil {
				fatal("look up seed user failed", "err", err)
			}
			if u != nil {
				seedUserID = u.ID
			}
		}
	}

	seed := make([]checks.Check, len(cfg.Checks))
	policy := network.Policy{AllowPrivateTargets: cfg.AllowPrivateTargets}
	for i, c := range cfg.Checks {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		check, err := c.NormalizeAndValidate(ctx, policy, true)
		if err != nil {
			cancel()
			fatal("configured check is invalid", "check_id", c.ID, "err", err)
		}
		cancel()
		seed[i] = check
	}
	if err := db.SeedChecks(seed, seedUserID); err != nil {
		fatal("seed checks failed", "err", err)
	}

	monitoringRuntime, err := monitoring.LoadRuntime(db)
	if err != nil {
		fatal("load monitoring runtime failed", "err", err)
	}

	h := server.New(db, monitoringRuntime, cfg)
	defer h.Close()

	go staleProbeLoop(db)
	go evictionLoop(db, cfg.RetentionDays)

	addr := ":8080"
	logger.Info("server listening", "addr", addr)
	if err := newHTTPServer(addr, h.Routes()).ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("server stopped unexpectedly", "err", err)
	}
}
