package main

import (
	"log/slog"
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
			slog.Default().Error("evict old results failed", "component", "eviction", "retention_days", retentionDays, "err", err)
			continue
		}
		if n > 0 {
			slog.Default().Info("evicted old results", "component", "eviction", "rows_deleted", n, "retention_days", retentionDays)
		}
	}
}
