package main

import (
	"log"
	"time"

	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/store"
)

const evictionInterval = 6 * time.Hour

func evictionLoop(db *store.Store, retentionDays int) {
	if retentionDays <= 0 {
		retentionDays = config.DefaultRetentionDays
	}
	for {
		time.Sleep(evictionInterval)
		cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
		n, err := db.EvictOldResults(cutoff)
		if err != nil {
			log.Printf("eviction: error: %s", err)
			continue
		}
		if n > 0 {
			log.Printf("eviction: deleted %d rows older than %d days", n, retentionDays)
		}
	}
}
