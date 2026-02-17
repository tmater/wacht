package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/check"
	"github.com/tmater/wacht/internal/proto"
)

func main() {
	probeID := flag.String("probe-id", "probe-local", "unique identifier for this probe")
	serverURL := flag.String("server", "http://localhost:8080", "wacht server URL")
	flag.Parse()

	log.Printf("wacht-probe starting probe-id=%s server=%s", *probeID, *serverURL)

	interval := 30 * time.Second

	checks := []struct {
		id     string
		target string
	}{
		{"check-1", "https://example.com"},
		{"check-2", "https://google.com"},
	}

	for {
		for _, c := range checks {
			result := check.HTTP(c.id, *probeID, c.target)
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
