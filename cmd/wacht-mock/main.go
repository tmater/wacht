package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// flapCounter is used by /flap to alternate between up and down responses.
// In Go, package-level variables are zero-initialized — this starts at 0.
// atomic.AddUint64 is like a thread-safe i++ in Java.
var flapCounter uint64

const (
	stateUp int32 = iota
	stateDown
)

type controlState struct {
	Status string `json:"status"`
}

func main() {
	var currentState atomic.Int32
	currentState.Store(stateUp)

	mux := http.NewServeMux()

	// /up — always returns 200. Use this as a baseline healthy target.
	mux.HandleFunc("/up", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// /down — always returns 503. Points checks here to simulate a down target.
	mux.HandleFunc("/down", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("down"))
	})

	// /slow — waits 5 seconds before responding. Useful for timeout testing.
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("slow ok"))
	})

	// /flap — alternates between 200 and 503 on each request.
	// atomic.AddUint64 returns the new value; odd = down, even = up.
	mux.HandleFunc("/flap", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddUint64(&flapCounter, 1)
		if n%2 == 0 {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("flap up"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("flap down"))
		}
	})

	// /state is both the check target and the control surface for smoke tests:
	// GET returns the current state as a normal health endpoint, and POST updates
	// that state so the real probes observe the transition on their next run.
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeState(w, currentState.Load())
		case http.MethodPost:
			var req controlState
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			switch req.Status {
			case "up":
				currentState.Store(stateUp)
			case "down":
				currentState.Store(stateDown)
			default:
				http.Error(w, "unsupported status", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	addr := ":9090"
	log.Printf("wacht-mock listening on %s", addr)
	log.Printf("endpoints: /up /down /slow /flap /state")
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("mock server error: %s", err)
	}
}

func writeState(w http.ResponseWriter, state int32) {
	if state == stateDown {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("down"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("up"))
}
