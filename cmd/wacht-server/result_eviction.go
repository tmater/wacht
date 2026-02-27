package main

import (
	"log"
	"time"

	"github.com/tmater/wacht/internal/store"
)

const evictionInterval = 6 * time.Hour
const defaultRetentionDays = 30

func evictionLoop(db *store.Store, retentionDays int) {
	if retentionDays <= 0 {
		retentionDays = defaultRetentionDays
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
