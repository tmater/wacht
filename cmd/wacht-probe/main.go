package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/tmater/wacht/internal/check"
	"github.com/tmater/wacht/internal/proto"
)

const serverURL = "http://localhost:8080"

func main() {
	log.Println("wacht-probe starting")

	probeID := "probe-local"
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
			result := check.HTTP(c.id, probeID, c.target)
			if err := postResult(result); err != nil {
				log.Printf("failed to post result: %s", err)
			}
		}

		log.Printf("sleeping %s until next round", interval)
		time.Sleep(interval)
	}
}

func postResult(result proto.CheckResult) error {
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
