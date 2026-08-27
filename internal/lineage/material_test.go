package lineage_test

import (
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
)

func TestLineageConvertConservation(t *testing.T) {
	l := lineage.NewLineage()
	parent := domain.MaterialID("PIPE-1")
	if err := l.AddNode(lineage.MaterialIdentity{
		ID: parent, Batch: "B1", Kind: domain.KindPipe, Length: 1000,
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	// Cut 1000mm into an installed stub, a sample and loss.
	if err := l.Convert(parent, "PIPE-1-INSTALL", domain.KindStub, 800); err != nil {
		t.Fatalf("Convert install: %v", err)
	}
	if err := l.Convert(parent, "PIPE-1-SAMPLE", domain.KindSample, 150); err != nil {
		t.Fatalf("Convert sample: %v", err)
	}
	if err := l.Convert(parent, "PIPE-1-LOSS", domain.KindLoss, 50); err != nil {
		t.Fatalf("Convert loss: %v", err)
	}
	if got := l.Remaining(parent); got != 0 {
		t.Fatalf("remaining = %d, want 0", got)
	}
	if ok, id := l.Conserved(); !ok {
		t.Fatalf("expected conserved lineage, violation at %s", id)
	}
}

func TestLineageConvertExceedsRemaining(t *testing.T) {
	l := lineage.NewLineage()
	parent := domain.MaterialID("PIPE-1")
	if err := l.AddNode(lineage.MaterialIdentity{ID: parent, Batch: "B1", Kind: domain.KindPipe, Length: 100}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := l.Convert(parent, "PIPE-1-A", domain.KindStub, 60); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if err := l.Convert(parent, "PIPE-1-B", domain.KindStub, 50); err == nil {
		t.Fatal("expected lineage breach when cut exceeds remaining")
	} else if se, ok := err.(*domain.StableError); !ok || se.Code != domain.CodeLineageBreach {
		t.Fatalf("expected CodeLineageBreach, got %v", err)
	}
}

func TestLineageDuplicateNodeRejected(t *testing.T) {
	l := lineage.NewLineage()
	id := domain.MaterialID("PIPE-1")
	_ = l.AddNode(lineage.MaterialIdentity{ID: id, Batch: "B1", Kind: domain.KindPipe, Length: 100})
	if err := l.AddNode(lineage.MaterialIdentity{ID: id, Batch: "B2", Kind: domain.KindPipe, Length: 100}); err == nil {
		t.Fatal("expected duplicate material id rejection")
	}
}

func TestLineageNegativeLengthRejected(t *testing.T) {
	l := lineage.NewLineage()
	err := l.AddNode(lineage.MaterialIdentity{ID: "PIPE-1", Batch: "B1", Kind: domain.KindPipe, Length: -1})
	if err == nil {
		t.Fatal("expected negative length rejection")
	}
}

func TestLeaseOverlaps(t *testing.T) {
	a := lineage.Lease{ResourceType: lineage.ResourceWelder, ResourceID: "W1", Start: 0, End: 100}
	b := lineage.Lease{ResourceType: lineage.ResourceWelder, ResourceID: "W1", Start: 50, End: 150}
	c := lineage.Lease{ResourceType: lineage.ResourceWelder, ResourceID: "W1", Start: 100, End: 200}
	if !a.Overlaps(b) {
		t.Fatal("expected a and b to overlap")
	}
	if a.Overlaps(c) {
		t.Fatal("expected a and c not to overlap (adjacent)")
	}
}

func TestLeaseExpired(t *testing.T) {
	l := lineage.Lease{Start: 0, End: 100}
	if l.Expired(50) {
		t.Fatal("lease should not be expired at 50")
	}
	if !l.Expired(100) {
		t.Fatal("lease should be expired at 100")
	}
	l.Released = true
	if l.Expired(200) {
		t.Fatal("released lease should not be reported as expired")
	}
}
