package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tmater/wacht/internal/alert"
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

type webhookControlState struct {
	FailNext int `json:"fail_next"`
}

func main() {
	var currentState atomic.Int32
	currentState.Store(stateUp)
	var webhooks webhookStore
	tcpTarget, err := newTCPTarget(":9091")
	if err != nil {
		log.Fatalf("failed to start tcp target: %s", err)
	}
	defer tcpTarget.close()
	dnsTarget, err := newDNSTarget(":53", "127.0.0.11:53", dnsFixtureHost, dnsFixtureIP)
	if err != nil {
		log.Fatalf("failed to start dns target: %s", err)
	}
	defer dnsTarget.close()

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

	// /http/state is both the check target and the control surface for smoke
	// tests: GET returns the current state as a normal health endpoint, and POST
	// updates that state so the real probes observe the transition on their next
	// run.
	httpStateHandler := func(w http.ResponseWriter, r *http.Request) {
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
	}
	mux.HandleFunc("/http/state", httpStateHandler)

	// /tcp/state is the HTTP control surface for the toggleable TCP listener
	// used by the TCP smoke scenario.
	mux.HandleFunc("/tcp/state", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, controlState{Status: tcpTarget.status()})
		case http.MethodPost:
			var req controlState
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if err := tcpTarget.setStatus(req.Status); err != nil {
				if errors.Is(err, errUnsupportedStatus) {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
				log.Printf("failed to update tcp target status: %s", err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// /dns/state toggles the smoke-only DNS fixture between returning a single
	// A record and NXDOMAIN for the fixture hostname.
	mux.HandleFunc("/dns/state", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, controlState{Status: dnsTarget.status()})
		case http.MethodPost:
			var req controlState
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if err := dnsTarget.setStatus(req.Status); err != nil {
				if errors.Is(err, errUnsupportedStatus) {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
				log.Printf("failed to update dns target status: %s", err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// /webhook stores received webhook payloads so smoke scenarios can assert
	// real end-to-end delivery through the server's alert sender.
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(webhooks.list()); err != nil {
				http.Error(w, "encode error", http.StatusInternalServerError)
			}
		case http.MethodPost:
			var payload alert.AlertPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if webhooks.failNow() {
				http.Error(w, "forced failure", http.StatusInternalServerError)
				return
			}
			webhooks.add(payload)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			webhooks.clear()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/webhook/control", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, webhooks.control())
		case http.MethodPost:
			var req webhookControlState
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if req.FailNext < 0 {
				http.Error(w, "fail_next must be non-negative", http.StatusBadRequest)
				return
			}
			webhooks.setFailNext(req.FailNext)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	addr := ":9090"
	log.Printf("wacht-mock listening on %s", addr)
	log.Printf("endpoints: /up /down /slow /flap /http/state /tcp/state /dns/state /webhook /webhook/control")
	log.Printf("tcp target listening on %s", tcpTarget.addr)
	log.Printf("dns target serving %s -> %s", dnsFixtureHost, dnsFixtureIP)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("mock server error: %s", err)
	}
}

var errUnsupportedStatus = errors.New("unsupported status")

type tcpTarget struct {
	mu       sync.Mutex
	addr     string
	listener net.Listener
}

func newTCPTarget(addr string) (*tcpTarget, error) {
	target := &tcpTarget{addr: addr}
	if err := target.setStatus("up"); err != nil {
		return nil, err
	}
	return target, nil
}

func (t *tcpTarget) status() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener == nil {
		return "down"
	}
	return "up"
}

func (t *tcpTarget) setStatus(status string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch status {
	case "up":
		if t.listener != nil {
			return nil
		}
		ln, err := net.Listen("tcp", t.addr)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", t.addr, err)
		}
		t.listener = ln
		go t.acceptLoop(ln)
		return nil
	case "down":
		if t.listener == nil {
			return nil
		}
		err := t.listener.Close()
		t.listener = nil
		if err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("close listener on %s: %w", t.addr, err)
		}
		return nil
	default:
		return fmt.Errorf("%w %q", errUnsupportedStatus, status)
	}
}

func (t *tcpTarget) close() error {
	return t.setStatus("down")
}

func (t *tcpTarget) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("tcp target accept error on %s: %s", t.addr, err)
			return
		}
		conn.Close()
	}
}

type webhookStore struct {
	mu       sync.Mutex
	payloads []alert.AlertPayload
	failNext int
}

func (s *webhookStore) add(payload alert.AlertPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.payloads = append(s.payloads, payload)
}

func (s *webhookStore) list() []alert.AlertPayload {
	s.mu.Lock()
	defer s.mu.Unlock()

	payloads := make([]alert.AlertPayload, len(s.payloads))
	copy(payloads, s.payloads)
	return payloads
}

func (s *webhookStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.payloads = nil
	s.failNext = 0
}

func (s *webhookStore) setFailNext(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failNext = n
}

func (s *webhookStore) control() webhookControlState {
	s.mu.Lock()
	defer s.mu.Unlock()

	return webhookControlState{FailNext: s.failNext}
}

func (s *webhookStore) failNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failNext <= 0 {
		return false
	}
	s.failNext--
	return true
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
	}
}
