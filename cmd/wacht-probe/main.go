package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/logx"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

func main() {
	logger := logx.Configure("wacht-probe")
	configPath := flag.String("config", "probe.yaml", "path to probe config file")
	serverOverride := flag.String("server", "", "override server URL from config")
	flag.Parse()

	fatal := func(msg string, args ...any) {
		logger.Error(msg, args...)
		os.Exit(1)
	}

	cfg, err := config.LoadProbe(*configPath)
	if err != nil {
		fatal("load config failed", "config_path", *configPath, "err", err)
	}

	if *serverOverride != "" {
		cfg.Server = *serverOverride
	}

	logger.Info("probe starting", "probe_id", cfg.ProbeID, "server", cfg.Server, "config_path", *configPath)

	apiClient := probeapi.NewClient(cfg.Server, cfg.ProbeID, cfg.Secret, nil)

	if err := apiClient.Register(context.Background(), "dev"); err != nil {
		fatal("register probe failed", "probe_id", cfg.ProbeID, "err", err)
	}
	logger.Info("probe registered", "probe_id", cfg.ProbeID)

	checkList, err := apiClient.FetchChecks(context.Background())
	if err != nil {
		fatal("fetch checks failed", "probe_id", cfg.ProbeID, "err", err)
	}
	logger.Info("checks fetched", "probe_id", cfg.ProbeID, "count", len(checkList))

	policy := network.Policy{AllowPrivateTargets: cfg.AllowPrivateTargets}
	resultBatcher := newResultBatcher(apiClient, cfg.ResultFlushInterval, defaultResultBatchMaxSize)
	defer resultBatcher.Close()

	scheduler := newScheduler(cfg, policy, resultBatcher)
	defer scheduler.Close()
	scheduler.Reconcile(checkList)

	// TODO: Split heartbeat and check-sync into separate config intervals if the
	// probe needs different liveness and config propagation cadences.
	go heartbeatLoop(apiClient, cfg.ProbeID, cfg.HeartbeatInterval)
	checkSyncLoop(apiClient, cfg.ProbeID, cfg.HeartbeatInterval, scheduler.Reconcile)
}

// heartbeatLoop only reports probe liveness
func heartbeatLoop(apiClient *probeapi.Client, probeID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if err := apiClient.Heartbeat(context.Background()); err != nil {
			slog.Default().Warn("probe heartbeat failed", "component", "probe", "probe_id", probeID, "err", err)
			continue
		}
	}
}

// checkSyncLoop polls the server for the current check set and hands the
// result to the scheduler
func checkSyncLoop(apiClient *probeapi.Client, probeID string, interval time.Duration, onChecks func([]proto.ProbeCheck)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		updated, err := apiClient.FetchChecks(context.Background())
		if err != nil {
			slog.Default().Warn("check sync failed", "component", "probe", "probe_id", probeID, "err", err)
			continue
		}
		slog.Default().Debug("checks refreshed", "component", "probe", "probe_id", probeID, "count", len(updated))
		onChecks(updated)
	}
}
