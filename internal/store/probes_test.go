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

func TestProbeStatuses_ScopedToUser(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedProbes([]ProbeSeed{
		{ProbeID: "probe-a", Secret: "secret-a"},
		{ProbeID: "probe-b", Secret: "secret-b"},
		{ProbeID: "probe-c", Secret: "secret-c"},
	}); err != nil {
		t.Fatalf("SeedProbes: %v", err)
	}
	if err := s.RegisterProbe("probe-a", "v1.0.0"); err != nil {
		t.Fatalf("RegisterProbe probe-a: %v", err)
	}
	if err := s.RegisterProbe("probe-b", "v1.0.0"); err != nil {
		t.Fatalf("RegisterProbe probe-b: %v", err)
	}
	if err := s.RegisterProbe("probe-c", "v1.0.0"); err != nil {
		t.Fatalf("RegisterProbe probe-c: %v", err)
	}

	alice, err := s.CreateUser("alice-probes@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := s.CreateUser("bob-probes@example.com", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	if err := s.CreateCheck(testCheck("alice-check", "http", "https://alice.example.com"), alice.ID); err != nil {
		t.Fatalf("CreateCheck alice: %v", err)
	}
	if err := s.CreateCheck(testCheck("bob-check", "http", "https://bob.example.com"), bob.ID); err != nil {
		t.Fatalf("CreateCheck bob: %v", err)
	}

	saveResult(t, s, "alice-check", "probe-a", true)
	saveResult(t, s, "bob-check", "probe-b", true)

	aliceProbes, err := s.ProbeStatuses(alice.ID)
	if err != nil {
		t.Fatalf("ProbeStatuses alice: %v", err)
	}
	if len(aliceProbes) != 1 {
		t.Fatalf("expected 1 alice probe, got %d", len(aliceProbes))
	}
	if aliceProbes[0].ProbeID != "probe-a" {
		t.Fatalf("expected probe-a, got %s", aliceProbes[0].ProbeID)
	}

	bobProbes, err := s.ProbeStatuses(bob.ID)
	if err != nil {
		t.Fatalf("ProbeStatuses bob: %v", err)
	}
	if len(bobProbes) != 1 {
		t.Fatalf("expected 1 bob probe, got %d", len(bobProbes))
	}
	if bobProbes[0].ProbeID != "probe-b" {
		t.Fatalf("expected probe-b, got %s", bobProbes[0].ProbeID)
	}
}
