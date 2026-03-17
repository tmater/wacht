package main

import (
	"context"
	"flag"
	"log"
	"time"

	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

func main() {
	configPath := flag.String("config", "probe.yaml", "path to probe config file")
	serverOverride := flag.String("server", "", "override server URL from config")
	flag.Parse()

	cfg, err := config.LoadProbe(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %s", err)
	}

	if *serverOverride != "" {
		cfg.Server = *serverOverride
	}

	log.Printf("wacht-probe starting probe-id=%s server=%s config=%s", cfg.ProbeID, cfg.Server, *configPath)

	apiClient := probeapi.NewClient(cfg.Server, cfg.ProbeID, cfg.Secret, nil)

	if err := apiClient.Register(context.Background(), "dev"); err != nil {
		log.Fatalf("probe: failed to register with server: %s", err)
	}
	log.Printf("probe: registered with server as probe_id=%s", cfg.ProbeID)

	checkList, err := apiClient.FetchChecks(context.Background())
	if err != nil {
		log.Fatalf("probe: failed to fetch checks from server: %s", err)
	}
	log.Printf("probe: fetched %d checks from server", len(checkList))

	policy := network.Policy{AllowPrivateTargets: cfg.AllowPrivateTargets}
	scheduler := newScheduler(cfg, policy, apiClient)
	defer scheduler.Close()
	scheduler.Reconcile(checkList)

	// TODO: Split heartbeat and check-sync into separate config intervals if the
	// probe needs different liveness and config propagation cadences.
	go heartbeatLoop(apiClient, cfg.HeartbeatInterval)
	checkSyncLoop(apiClient, cfg.HeartbeatInterval, scheduler.Reconcile)
}

// heartbeatLoop only reports probe liveness
func heartbeatLoop(apiClient *probeapi.Client, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if err := apiClient.Heartbeat(context.Background()); err != nil {
			log.Printf("probe: probe-server API heartbeat failed: %s", err)
			continue
		}
	}
}

// checkSyncLoop polls the server for the current check set and hands the
// result to the scheduler
func checkSyncLoop(apiClient *probeapi.Client, interval time.Duration, onChecks func([]proto.ProbeCheck)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		updated, err := apiClient.FetchChecks(context.Background())
		if err != nil {
			log.Printf("probe: probe-server API check sync failed: %s", err)
			continue
		}
		log.Printf("probe: refreshed %d checks from server", len(updated))
		onChecks(updated)
	}
}
