package main

import (
	"log/slog"
	"time"

	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/monitoring"
	"github.com/tmater/wacht/internal/store"
)

const (
	probeSweepInterval     = 5 * time.Second
	checkSweepInterval     = 1 * time.Second
	checkSweepStartupGrace = 10 * time.Second
)

func probeSweepLoop(db *store.Store, runtime *monitoring.Runtime, offlineAfter time.Duration) {
	if offlineAfter <= 0 {
		offlineAfter = config.DefaultProbeOfflineAfter
	}

	ticker := time.NewTicker(probeSweepInterval)
	defer ticker.Stop()

	for range ticker.C {
		expired, err := monitoring.SweepProbes(runtime, db, time.Now().UTC(), offlineAfter)
		if err != nil {
			slog.Default().Error("probe sweep failed", "component", "monitoring_probe_sweeper", "err", err)
			continue
		}
		if expired > 0 {
			slog.Default().Info("expired stale probes", "component", "monitoring_probe_sweeper", "count", expired)
		}
	}
}

func checkSweepLoop(db *store.Store, runtime *monitoring.Runtime) {
	// Let replayed runtime evidence survive long enough for probes to reconnect
	// and submit fresh results after a server restart.
	time.Sleep(checkSweepStartupGrace)

	ticker := time.NewTicker(checkSweepInterval)
	defer ticker.Stop()

	for range ticker.C {
		expired, err := monitoring.SweepChecks(runtime, db, time.Now().UTC())
		if err != nil {
			slog.Default().Error("check sweep failed", "component", "monitoring_check_sweeper", "err", err)
			continue
		}
		if expired > 0 {
			slog.Default().Info("expired stale check evidence", "component", "monitoring_check_sweeper", "count", expired)
		}
	}
}
