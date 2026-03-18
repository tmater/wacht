package main

import (
	"log/slog"
	"time"

	"github.com/tmater/wacht/internal/store"
)

const staleThreshold = 2 * time.Minute

func staleProbeLoop(db *store.Store) {
	for {
		time.Sleep(30 * time.Second)
		statuses, err := db.AllProbeStatuses()
		if err != nil {
			slog.Default().Error("query probe statuses failed", "component", "probe_health", "err", err)
			continue
		}
		for _, ps := range statuses {
			if ps.LastSeenAt == nil {
				slog.Default().Warn("probe has never checked in", "component", "probe_health", "probe_id", ps.ProbeID)
				continue
			}
			if time.Since(*ps.LastSeenAt) > staleThreshold {
				slog.Default().Warn("probe is stale", "component", "probe_health", "probe_id", ps.ProbeID, "last_seen_ago", time.Since(*ps.LastSeenAt).Round(time.Second).String())
			}
		}
	}
}
