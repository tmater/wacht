package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/server"
	"github.com/tmater/wacht/internal/store"
)

func main() {
	configPath := flag.String("config", "wacht.yaml", "path to config file")
	dbPath := flag.String("db", "wacht.db", "path to SQLite database file")
	flag.Parse()

	log.Println("wacht-server starting")

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %s", err)
	}

	db, err := store.New(*dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %s", err)
	}
	defer db.Close()

	h := server.New(db, cfg)

	addr := ":8080"
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Routes()); err != nil {
		log.Fatalf("server error: %s", err)
	}
}
