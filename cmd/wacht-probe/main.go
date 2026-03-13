package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

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

	if err := register(cfg.Server, cfg.Secret, cfg.ProbeID); err != nil {
		log.Fatalf("probe: failed to register with server: %s", err)
	}

	checkList, err := fetchChecks(cfg.Server, cfg.Secret, cfg.ProbeID)
	if err != nil {
		log.Fatalf("probe: failed to fetch checks from server: %s", err)
	}
	log.Printf("probe: fetched %d checks from server", len(checkList))

	policy := network.Policy{AllowPrivateTargets: cfg.AllowPrivateTargets}
	scheduler := newScheduler(cfg, policy)
	defer scheduler.Close()
	scheduler.Reconcile(checkList)

	// TODO: Split heartbeat and check-sync into separate config intervals if the
	// probe needs different liveness and config propagation cadences.
	go heartbeatLoop(cfg.Server, cfg.Secret, cfg.ProbeID, cfg.HeartbeatInterval)
	checkSyncLoop(cfg.Server, cfg.Secret, cfg.ProbeID, cfg.HeartbeatInterval, scheduler.Reconcile)
}

func setProbeHeaders(req *http.Request, probeID, secret string) {
	req.Header.Set(probeIDHeader, probeID)
	req.Header.Set(probeSecretHeader, secret)
}

const (
	probeIDHeader     = "X-Wacht-Probe-ID"
	probeSecretHeader = "X-Wacht-Probe-Secret"
)

func register(serverURL, secret, probeID string) error {
	body, err := json.Marshal(map[string]string{"version": "dev"})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", serverURL+"/api/probes/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setProbeHeaders(req, probeID, secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	log.Printf("probe: registered with server as probe_id=%s", probeID)
	return nil
}

func postResult(serverURL, secret string, result proto.CheckResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", serverURL+"/api/results", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setProbeHeaders(req, result.ProbeID, secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log.Printf("posted result: check_id=%s up=%v status=%d", result.CheckID, result.Up, resp.StatusCode)
	return nil
}
