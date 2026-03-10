package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/tmater/wacht/internal/checks"
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

	var (
		mu       sync.Mutex
		cancelFn context.CancelFunc
	)

	startScheduler := func(cs []checks.Check) {
		mu.Lock()
		if cancelFn != nil {
			cancelFn()
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancelFn = cancel
		mu.Unlock()

		for _, c := range cs {
			check := c
			interval := time.Duration(check.Interval) * time.Second
			go func() {
				runAndPost(cfg, policy, check)
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						runAndPost(cfg, policy, check)
					case <-ctx.Done():
						return
					}
				}
			}()
		}
	}

	startScheduler(checkList)

	heartbeatLoop(cfg.Server, cfg.Secret, cfg.ProbeID, cfg.HeartbeatInterval, func() {
		updated, err := fetchChecks(cfg.Server, cfg.Secret, cfg.ProbeID)
		if err != nil {
			log.Printf("probe: failed to refresh checks: %s", err)
			return
		}
		log.Printf("probe: refreshed %d checks from server", len(updated))
		startScheduler(updated)
	})
}

func runAndPost(cfg *config.ProbeConfig, policy network.Policy, check checks.Check) {
	var result proto.CheckResult
	switch check.Type {
	case checks.CheckHTTP:
		result = checks.HTTP(check.ID, cfg.ProbeID, check.Target, policy)
	case checks.CheckTCP:
		result = checks.TCP(check.ID, cfg.ProbeID, check.Target, policy)
	case checks.CheckDNS:
		result = checks.DNS(check.ID, cfg.ProbeID, check.Target, policy)
	default:
		log.Printf("probe: unknown check type %q for check_id=%s, skipping", check.Type, check.ID)
		return
	}
	if err := postResult(cfg.Server, cfg.Secret, result); err != nil {
		log.Printf("failed to post result: %s", err)
	}
}

func setProbeHeaders(req *http.Request, probeID, secret string) {
	req.Header.Set(probeIDHeader, probeID)
	req.Header.Set(probeSecretHeader, secret)
}

const (
	probeIDHeader     = "X-Wacht-Probe-ID"
	probeSecretHeader = "X-Wacht-Probe-Secret"
)

func heartbeatLoop(serverURL, secret, probeID string, interval time.Duration, onHeartbeat func()) {
	for {
		time.Sleep(interval)
		req, err := http.NewRequest("POST", serverURL+"/api/probes/heartbeat", nil)
		if err != nil {
			log.Printf("probe: heartbeat error: %s", err)
			continue
		}
		setProbeHeaders(req, probeID, secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("probe: heartbeat error: %s", err)
			continue
		}
		resp.Body.Close()
		log.Printf("probe: heartbeat sent probe_id=%s status=%d", probeID, resp.StatusCode)
		onHeartbeat()
	}
}

func fetchChecks(serverURL, secret, probeID string) ([]checks.Check, error) {
	req, err := http.NewRequest("GET", serverURL+"/api/probes/checks", nil)
	if err != nil {
		return nil, err
	}
	setProbeHeaders(req, probeID, secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var checks []checks.Check
	if err := json.NewDecoder(resp.Body).Decode(&checks); err != nil {
		return nil, err
	}
	return checks, nil
}

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
