package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/server"
	"github.com/tmater/wacht/internal/store"
)

const staleThreshold = 2 * time.Minute

func staleProbeLoop(db *store.Store) {
	for {
		time.Sleep(30 * time.Second)
		statuses, err := db.AllProbeStatuses()
		if err != nil {
			log.Printf("stale check: failed to query probes: %s", err)
			continue
		}
		for _, ps := range statuses {
			if time.Since(ps.LastSeenAt) > staleThreshold {
				log.Printf("stale probe: probe_id=%s last_seen=%s ago", ps.ProbeID, time.Since(ps.LastSeenAt).Round(time.Second))
			}
		}
	}
}

func main() {
	configPath := flag.String("config", "server.yaml", "path to server config file")
	dbPath := flag.String("db", "wacht.db", "path to SQLite database file")
	flag.Parse()

	log.Println("wacht-server starting")

	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %s", err)
	}

	db, err := store.New(*dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %s", err)
	}
	defer db.Close()

	seed := make([]store.Check, len(cfg.Checks))
	for i, c := range cfg.Checks {
		seed[i] = store.Check{ID: c.ID, Type: c.Type, Target: c.Target, Webhook: c.Webhook}
	}
	if err := db.SeedChecks(seed); err != nil {
		log.Fatalf("failed to seed checks: %s", err)
	}

	if cfg.SeedUser.Email != "" && cfg.SeedUser.Password != "" {
		exists, err := db.UserExists()
		if err != nil {
			log.Fatalf("failed to check for existing users: %s", err)
		}
		if !exists {
			if _, err := db.CreateUser(cfg.SeedUser.Email, cfg.SeedUser.Password); err != nil {
				log.Fatalf("failed to seed user: %s", err)
			}
			log.Printf("seeded dev user: %s", cfg.SeedUser.Email)
		}
	}

	h := server.New(db, cfg)

	go staleProbeLoop(db)

	addr := ":8080"
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Routes()); err != nil {
		log.Fatalf("server error: %s", err)
	}
}
