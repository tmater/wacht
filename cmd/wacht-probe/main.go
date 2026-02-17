package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/check"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/proto"
)

func main() {
	probeID := flag.String("probe-id", "probe-local", "unique identifier for this probe")
	serverURL := flag.String("server", "http://localhost:8080", "wacht server URL")
	configPath := flag.String("config", "wacht.yaml", "path to config file")
	flag.Parse()

	log.Printf("wacht-probe starting probe-id=%s server=%s config=%s", *probeID, *serverURL, *configPath)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %s", err)
	}

	log.Printf("loaded %d checks", len(cfg.Checks))

	interval := 30 * time.Second

	for {
		for _, c := range cfg.Checks {
			var result proto.CheckResult
			switch c.Type {
			case "http", "":
				result = check.HTTP(c.ID, *probeID, c.Target)
			default:
				log.Printf("probe: unknown check type %q for check_id=%s, skipping", c.Type, c.ID)
				continue
			}

			if err := postResult(*serverURL, result); err != nil {
				log.Printf("failed to post result: %s", err)
			}
		}

		log.Printf("sleeping %s until next round", interval)
		time.Sleep(interval)
	}
}

func postResult(serverURL string, result proto.CheckResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}

	resp, err := http.Post(serverURL+"/api/results", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log.Printf("posted result: check_id=%s up=%v status=%d", result.CheckID, result.Up, resp.StatusCode)
	return nil
}
