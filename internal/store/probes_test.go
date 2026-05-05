package store

import (
	"errors"
	"testing"
)

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

func TestCreateProbeCredential_AuthenticateProbe(t *testing.T) {
	s := newTestStore(t)

	credential, err := s.CreateProbeCredential("probe-api-1")
	if err != nil {
		t.Fatalf("CreateProbeCredential: %v", err)
	}
	if credential.ProbeID != "probe-api-1" {
		t.Fatalf("ProbeID = %q, want probe-api-1", credential.ProbeID)
	}
	if credential.Secret == "" {
		t.Fatal("expected generated secret")
	}

	probe, err := s.AuthenticateProbe("probe-api-1", credential.Secret)
	if err != nil {
		t.Fatalf("AuthenticateProbe: %v", err)
	}
	if probe == nil {
		t.Fatal("expected probe, got nil")
	}
}

func TestCreateProbeCredential_GeneratesProbeID(t *testing.T) {
	s := newTestStore(t)

	credential, err := s.CreateProbeCredential("")
	if err != nil {
		t.Fatalf("CreateProbeCredential: %v", err)
	}
	if credential.ProbeID == "" {
		t.Fatal("expected generated probe id")
	}
	if credential.Secret == "" {
		t.Fatal("expected generated secret")
	}
}

func TestCreateProbeCredential_RejectsDuplicateProbeID(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateProbeCredential("probe-api-1"); err != nil {
		t.Fatalf("CreateProbeCredential first: %v", err)
	}
	if _, err := s.CreateProbeCredential("probe-api-1"); !errors.Is(err, ErrProbeAlreadyExists) {
		t.Fatalf("CreateProbeCredential duplicate error = %v, want ErrProbeAlreadyExists", err)
	}
}

func TestCreateProbeCredential_RejectsInvalidProbeID(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateProbeCredential("bad probe"); !errors.Is(err, ErrInvalidProbeID) {
		t.Fatalf("CreateProbeCredential error = %v, want ErrInvalidProbeID", err)
	}
}

func TestSeedProbes_DoesNotRevokeAPICreatedProbe(t *testing.T) {
	s := newTestStore(t)

	credential, err := s.CreateProbeCredential("probe-api-1")
	if err != nil {
		t.Fatalf("CreateProbeCredential: %v", err)
	}
	if err := s.SeedProbes([]ProbeSeed{{ProbeID: "probe-config-1", Secret: "secret-1"}}); err != nil {
		t.Fatalf("SeedProbes: %v", err)
	}

	probe, err := s.AuthenticateProbe("probe-api-1", credential.Secret)
	if err != nil {
		t.Fatalf("AuthenticateProbe api probe: %v", err)
	}
	if probe == nil {
		t.Fatal("expected API-created probe to remain active after config seeding")
	}
}
