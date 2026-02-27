package main

import (
	"log"
	"time"

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
