package main

import (
	"log"
	"time"

	"github.com/tmater/wacht/internal/check"
)

func main() {
	log.Println("wacht-probe starting")

	probeID := "probe-local"
	interval := 30 * time.Second

	checks := []struct {
		id     string
		target string
	}{
		{"check-1", "https://example.com"},
		{"check-2", "https://google.com"},
	}

	for {
		for _, c := range checks {
			result := check.HTTP(c.id, probeID, c.target)
			log.Printf("result: check_id=%s up=%v latency=%s error=%s",
				result.CheckID, result.Up, result.Latency, result.Error)
		}

		log.Printf("sleeping %s until next round", interval)
		time.Sleep(interval)
	}
}
