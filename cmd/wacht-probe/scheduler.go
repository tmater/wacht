package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

// runningCheck tracks one live worker so reconcile can stop or replace it by
// check ID instead of rebuilding the whole scheduler.
type runningCheck struct {
	check  checks.Check
	cancel context.CancelFunc
}

// scheduler owns the per-check workers that execute checks on their configured
// intervals.
type scheduler struct {
	mu          sync.Mutex
	running     map[string]runningCheck
	startWorker func(checks.Check) runningCheck
}

// TODO: This goroutine-per-check model is fine at small scale, but a central
// scheduler plus bounded worker pool will scale better once probes need to run
// large check sets with smoother execution spread.
func newScheduler(cfg *config.ProbeConfig, policy network.Policy) *scheduler {
	s := &scheduler{
		running: make(map[string]runningCheck),
	}
	s.startWorker = func(check checks.Check) runningCheck {
		return s.startRuntimeWorker(cfg, policy, check)
	}
	return s
}

// Reconcile applies the latest desired check set by only touching added,
// removed, or changed checks. Unchanged checks keep their existing cadence.
func (s *scheduler) Reconcile(latest []checks.Check) {
	next := make(map[string]checks.Check, len(latest))
	for _, check := range latest {
		next[check.ID] = check
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, job := range s.running {
		check, ok := next[id]
		if !ok {
			job.cancel()
			delete(s.running, id)
			continue
		}
		if job.check != check {
			job.cancel()
			s.running[id] = s.startWorker(check)
		}
		delete(next, id)
	}

	for id, check := range next {
		s.running[id] = s.startWorker(check)
	}
}

// Close stops every running worker during probe shutdown.
func (s *scheduler) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, job := range s.running {
		job.cancel()
		delete(s.running, id)
	}
}

// startRuntimeWorker runs the check once immediately, then continues on the
// configured interval until reconcile or shutdown cancels it.
func (s *scheduler) startRuntimeWorker(cfg *config.ProbeConfig, policy network.Policy, check checks.Check) runningCheck {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		runAndPost(cfg, policy, check)
		ticker := time.NewTicker(time.Duration(check.Interval) * time.Second)
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

	return runningCheck{
		check:  check,
		cancel: cancel,
	}
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
