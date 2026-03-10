package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/server"
	"github.com/tmater/wacht/internal/store"
)

func main() {
	configPath := flag.String("config", "server.yaml", "path to server config file")
	dsn := flag.String("dsn", "", "Postgres DSN (e.g. postgres://user:pass@host/db)")
	flag.Parse()

	log.Println("wacht-server starting")

	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %s", err)
	}

	if *dsn == "" {
		log.Fatalf("--dsn is required")
	}

	db, err := store.New(*dsn)
	if err != nil {
		log.Fatalf("failed to open database: %s", err)
	}
	defer db.Close()

	probes := make([]store.ProbeSeed, len(cfg.Probes))
	for i, probe := range cfg.Probes {
		probes[i] = store.ProbeSeed{ProbeID: probe.ID, Secret: probe.Secret}
	}
	if err := db.SeedProbes(probes); err != nil {
		log.Fatalf("failed to seed probe credentials: %s", err)
	}
	if len(probes) == 0 {
		log.Printf("warning: no probes configured; probe authentication will reject all requests")
	}

	var seedUserID int64
	if cfg.SeedUser.Email != "" && cfg.SeedUser.Password != "" {
		exists, err := db.UserExists()
		if err != nil {
			log.Fatalf("failed to check for existing users: %s", err)
		}
		if !exists {
			u, err := db.CreateAdminUser(cfg.SeedUser.Email, cfg.SeedUser.Password)
			if err != nil {
				log.Fatalf("failed to seed user: %s", err)
			}
			log.Printf("seeded dev user: %s", cfg.SeedUser.Email)
			seedUserID = u.ID
		} else {
			u, err := db.AuthenticateUser(cfg.SeedUser.Email, cfg.SeedUser.Password)
			if err != nil {
				log.Fatalf("failed to look up seed user: %s", err)
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
			log.Fatalf("invalid configured check %q: %s", c.ID, err)
		}
		cancel()
		seed[i] = check
	}
	if err := db.SeedChecks(seed, seedUserID); err != nil {
		log.Fatalf("failed to seed checks: %s", err)
	}

	h := server.New(db, cfg)
	defer h.Close()

	go staleProbeLoop(db)
	go evictionLoop(db, cfg.RetentionDays)

	addr := ":8080"
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Routes()); err != nil {
		log.Fatalf("server error: %s", err)
	}
}
