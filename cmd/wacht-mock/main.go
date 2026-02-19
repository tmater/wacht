package main

import (
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// flapCounter is used by /flap to alternate between up and down responses.
// In Go, package-level variables are zero-initialized — this starts at 0.
// atomic.AddUint64 is like a thread-safe i++ in Java.
var flapCounter uint64

func main() {
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

	addr := ":9090"
	log.Printf("wacht-mock listening on %s", addr)
	log.Printf("endpoints: /up /down /slow /flap")
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("mock server error: %s", err)
	}
}
