package main

import (
	"log"
	"net/http"

	"github.com/tmater/wacht/internal/server"
	"github.com/tmater/wacht/internal/store"
)

func main() {
	log.Println("wacht-server starting")

	db, err := store.New("wacht.db")
	if err != nil {
		log.Fatalf("failed to open database: %s", err)
	}
	defer db.Close()

	h := server.New(db)

	addr := ":8080"
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Routes()); err != nil {
		log.Fatalf("server error: %s", err)
	}
}
