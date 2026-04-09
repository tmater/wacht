package store

import "testing"

func TestSeedProbes_AuthenticateProbe(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedProbes([]ProbeSeed{{ProbeID: "probe-1", Secret: "secret-1"}}); err != nil {
		t.Fatalf("SeedProbes: %v", err)
	}

	probe, err := s.AuthenticateProbe("probe-1", "secret-1")
	if err != nil {
		t.Fatalf("AuthenticateProbe: %v", err)
	}
	if probe == nil {
		t.Fatal("expected probe, got nil")
	}
	if probe.ProbeID != "probe-1" {
		t.Fatalf("expected probe-1, got %q", probe.ProbeID)
	}
	if probe.LastSeenAt != nil {
		t.Fatal("expected pre-provisioned probe to have nil LastSeenAt")
	}
}

func TestAuthenticateProbe_WrongSecret(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedProbes([]ProbeSeed{{ProbeID: "probe-1", Secret: "secret-1"}}); err != nil {
		t.Fatalf("SeedProbes: %v", err)
	}

	probe, err := s.AuthenticateProbe("probe-1", "wrong-secret")
	if err != nil {
		t.Fatalf("AuthenticateProbe: %v", err)
	}
	if probe != nil {
		t.Fatal("expected nil for wrong secret")
	}
}

func TestRegisterProbe_UpdatesVersionAndLastSeen(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedProbes([]ProbeSeed{{ProbeID: "probe-1", Secret: "secret-1"}}); err != nil {
		t.Fatalf("SeedProbes: %v", err)
	}

	if err := s.RegisterProbe("probe-1", "v1.2.3"); err != nil {
		t.Fatalf("RegisterProbe: %v", err)
	}

	probe, err := s.AuthenticateProbe("probe-1", "secret-1")
	if err != nil {
		t.Fatalf("AuthenticateProbe: %v", err)
	}
	if probe == nil {
		t.Fatal("expected probe, got nil")
	}
	if probe.Version != "v1.2.3" {
		t.Fatalf("expected version v1.2.3, got %q", probe.Version)
	}
	if probe.LastSeenAt == nil {
		t.Fatal("expected RegisterProbe to set LastSeenAt")
	}
}

func TestSeedProbes_RevokesMissingProbe(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedProbes([]ProbeSeed{
		{ProbeID: "probe-1", Secret: "secret-1"},
		{ProbeID: "probe-2", Secret: "secret-2"},
	}); err != nil {
		t.Fatalf("SeedProbes initial: %v", err)
	}

	if err := s.SeedProbes([]ProbeSeed{{ProbeID: "probe-1", Secret: "secret-1"}}); err != nil {
		t.Fatalf("SeedProbes second: %v", err)
	}

	probe, err := s.AuthenticateProbe("probe-2", "secret-2")
	if err != nil {
		t.Fatalf("AuthenticateProbe revoked probe: %v", err)
	}
	if probe != nil {
		t.Fatal("expected revoked probe to fail authentication")
	}
}

func TestActiveProbeIDs_OnlyReturnsActiveProbes(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedProbes([]ProbeSeed{
		{ProbeID: "probe-b", Secret: "secret-b"},
		{ProbeID: "probe-a", Secret: "secret-a"},
		{ProbeID: "probe-c", Secret: "secret-c"},
	}); err != nil {
		t.Fatalf("SeedProbes initial: %v", err)
	}

	if err := s.SeedProbes([]ProbeSeed{
		{ProbeID: "probe-b", Secret: "secret-b"},
		{ProbeID: "probe-a", Secret: "secret-a"},
	}); err != nil {
		t.Fatalf("SeedProbes second: %v", err)
	}

	ids, err := s.ActiveProbeIDs()
	if err != nil {
		t.Fatalf("ActiveProbeIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("len(ids) = %d, want 2", len(ids))
	}
	if ids[0] != "probe-a" || ids[1] != "probe-b" {
		t.Fatalf("ids = %v, want [probe-a probe-b]", ids)
	}
}
