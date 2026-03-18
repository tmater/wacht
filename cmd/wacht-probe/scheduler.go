package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	probeapi "github.com/tmater/wacht/internal/api/probe"
	"github.com/tmater/wacht/internal/checks"
	"github.com/tmater/wacht/internal/config"
	"github.com/tmater/wacht/internal/network"
	"github.com/tmater/wacht/internal/proto"
)

// runningCheck tracks one live worker so reconcile can stop or replace it by
// check ID instead of rebuilding the whole scheduler.
type runningCheck struct {
	check  proto.ProbeCheck
	cancel context.CancelFunc
}

// scheduler owns the per-check workers that execute checks on their configured
// intervals.
type scheduler struct {
	mu          sync.Mutex
	running     map[string]runningCheck
	startWorker func(proto.ProbeCheck) runningCheck
}

// TODO: This goroutine-per-check model is fine at small scale, but a central
// scheduler plus bounded worker pool will scale better once probes need to run
// large check sets with smoother execution spread.
func newScheduler(cfg *config.ProbeConfig, policy network.Policy, apiClient *probeapi.Client) *scheduler {
	s := &scheduler{
		running: make(map[string]runningCheck),
	}
	s.startWorker = func(check proto.ProbeCheck) runningCheck {
		return s.startRuntimeWorker(cfg, policy, apiClient, check)
	}
	return s
}

// Reconcile applies the latest desired check set by only touching added,
// removed, or changed checks. Unchanged checks keep their existing cadence.
func (s *scheduler) Reconcile(latest []proto.ProbeCheck) {
	next := make(map[string]proto.ProbeCheck, len(latest))
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
func (s *scheduler) startRuntimeWorker(cfg *config.ProbeConfig, policy network.Policy, apiClient *probeapi.Client, check proto.ProbeCheck) runningCheck {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		runAndPost(cfg, policy, apiClient, check)
		ticker := time.NewTicker(time.Duration(check.Interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				runAndPost(cfg, policy, apiClient, check)
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

func runAndPost(cfg *config.ProbeConfig, policy network.Policy, apiClient *probeapi.Client, check proto.ProbeCheck) {
	var result proto.CheckResult
	switch check.Type {
	case string(checks.CheckHTTP):
		result = checks.HTTP(check.ID, cfg.ProbeID, check.Target, policy)
	case string(checks.CheckTCP):
		result = checks.TCP(check.ID, cfg.ProbeID, check.Target, policy)
	case string(checks.CheckDNS):
		result = checks.DNS(check.ID, cfg.ProbeID, check.Target, policy)
	default:
		slog.Default().Warn("unknown check type; skipping", "component", "probe", "check_id", check.ID, "probe_id", cfg.ProbeID, "check_type", check.Type)
		return
	}
	if err := apiClient.PostResult(context.Background(), result); err != nil {
		slog.Default().Warn("result upload failed", "component", "probe", "check_id", check.ID, "probe_id", cfg.ProbeID, "err", err)
	}
}
