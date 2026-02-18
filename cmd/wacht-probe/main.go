package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/check"
	"github.com/tmater/wacht/internal/config"
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

	checks, err := fetchChecks(cfg.Server, cfg.Secret)
	if err != nil {
		log.Fatalf("probe: failed to fetch checks from server: %s", err)
	}
	log.Printf("probe: fetched %d checks from server", len(checks))

	go heartbeatLoop(cfg.Server, cfg.Secret, cfg.ProbeID, cfg.HeartbeatInterval)

	interval := 30 * time.Second

	for {
		for _, c := range checks {
			var result proto.CheckResult
			switch c.Type {
			case "http", "":
				result = check.HTTP(c.ID, cfg.ProbeID, c.Target)
			case "tcp":
				result = check.TCP(c.ID, cfg.ProbeID, c.Target)
			case "dns":
				result = check.DNS(c.ID, cfg.ProbeID, c.Target)
			default:
				log.Printf("probe: unknown check type %q for check_id=%s, skipping", c.Type, c.ID)
				continue
			}

			if err := postResult(cfg.Server, cfg.Secret, result); err != nil {
				log.Printf("failed to post result: %s", err)
			}
		}

		log.Printf("sleeping %s until next round", interval)
		time.Sleep(interval)
	}
}

func heartbeatLoop(serverURL, secret, probeID string, interval time.Duration) {
	for {
		time.Sleep(interval)
		body, _ := json.Marshal(map[string]string{"probe_id": probeID})
		req, err := http.NewRequest("POST", serverURL+"/api/probes/heartbeat", bytes.NewReader(body))
		if err != nil {
			log.Printf("probe: heartbeat error: %s", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Wacht-Secret", secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("probe: heartbeat error: %s", err)
			continue
		}
		resp.Body.Close()
		log.Printf("probe: heartbeat sent probe_id=%s status=%d", probeID, resp.StatusCode)
	}
}

func fetchChecks(serverURL, secret string) ([]config.Check, error) {
	req, err := http.NewRequest("GET", serverURL+"/api/probes/checks", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Wacht-Secret", secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var checks []config.Check
	if err := json.NewDecoder(resp.Body).Decode(&checks); err != nil {
		return nil, err
	}
	return checks, nil
}

func register(serverURL, secret, probeID string) error {
	body, err := json.Marshal(map[string]string{"probe_id": probeID, "version": "dev"})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", serverURL+"/api/probes/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wacht-Secret", secret)
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
	req.Header.Set("X-Wacht-Secret", secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log.Printf("posted result: check_id=%s up=%v status=%d", result.CheckID, result.Up, resp.StatusCode)
	return nil
}
