package catalog_test

import (
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/topology"
)

func validSnapshot() *catalog.RuleSnapshot {
	return &catalog.RuleSnapshot{
		Version: 1,
		DiameterTransitions: map[domain.DiameterMM][]domain.DiameterMM{
			110: {110},
			75:  {75},
		},
	}
}

// validGraph builds a fixed three-drain network where each drain reaches its
// own outlet through a single 110mm chain.
func validGraph() *topology.HydraulicGraph {
	g := &topology.HydraulicGraph{}
	for _, d := range []string{"D1", "D2", "D3"} {
		drainPort := domain.PortID("P_" + d)
		midPort := domain.PortID("P_" + d + "_S")
		outPort := domain.PortID("P_" + d + "_O")
		g.Drains = append(g.Drains, topology.RoofDrain{ID: domain.DrainID(d), Zone: domain.ZoneID("Z1")})
		g.Ports = append(g.Ports,
			topology.Port{ID: drainPort, Owner: d, Diameter: 110},
			topology.Port{ID: midPort, Owner: d + "_S", Diameter: 110},
			topology.Port{ID: outPort, Owner: "O" + d, Diameter: 110},
		)
		g.Outlets = append(g.Outlets, topology.Outlet{ID: domain.OutletID("O" + d)})
		g.Edges = append(g.Edges,
			topology.DirectedEdge{From: drainPort, To: midPort},
			topology.DirectedEdge{From: midPort, To: outPort},
		)
	}
	return g
}

func TestValidateTopologyValidThreeDrains(t *testing.T) {
	snap := validSnapshot()
	g := validGraph()
	if err := snap.ValidateTopology(g); err != nil {
		t.Fatalf("expected valid three-drain network, got %v", err)
	}
}

func TestValidateTopologyCycle(t *testing.T) {
	snap := validSnapshot()
	g := validGraph()
	// Inject a back edge from the D1 outlet back to the D1 drain.
	g.Edges = append(g.Edges, topology.DirectedEdge{
		From: domain.PortID("P_D1_O"),
		To:   domain.PortID("P_D1"),
	})
	err := snap.ValidateTopology(g)
	if err == nil {
		t.Fatal("expected cycle rejection")
	}
	if se, ok := err.(*domain.StableError); !ok || se.Code != domain.CodeCycle {
		t.Fatalf("expected CodeCycle, got %v", err)
	}
}

func TestValidateTopologyDuplicatePort(t *testing.T) {
	snap := validSnapshot()
	g := validGraph()
	g.Ports = append(g.Ports, topology.Port{ID: domain.PortID("P_D1"), Owner: "OTHER", Diameter: 110})
	err := snap.ValidateTopology(g)
	if err == nil {
		t.Fatal("expected duplicate port rejection")
	}
	if se, ok := err.(*domain.StableError); !ok || se.Code != domain.CodeDuplicatePort {
		t.Fatalf("expected CodeDuplicatePort, got %v", err)
	}
}

func TestValidateTopologyDisconnected(t *testing.T) {
	snap := validSnapshot()
	g := validGraph()
	// A drain with no declared port.
	g.Drains = append(g.Drains, topology.RoofDrain{ID: domain.DrainID("D4"), Zone: domain.ZoneID("Z1")})
	err := snap.ValidateTopology(g)
	if err == nil {
		t.Fatal("expected disconnected rejection")
	}
	if se, ok := err.(*domain.StableError); !ok || se.Code != domain.CodeDisconnected {
		t.Fatalf("expected CodeDisconnected, got %v", err)
	}
}

func TestValidateTopologyForkRejected(t *testing.T) {
	snap := validSnapshot()
	g := validGraph()
	// Fork: the D1 drain port gains a second outgoing edge.
	g.Edges = append(g.Edges, topology.DirectedEdge{
		From: domain.PortID("P_D1"),
		To:   domain.PortID("P_D2_S"),
	})
	err := snap.ValidateTopology(g)
	if err == nil {
		t.Fatal("expected fork rejection")
	}
	if se, ok := err.(*domain.StableError); !ok || se.Code != domain.CodeDisconnected {
		t.Fatalf("expected CodeDisconnected for fork, got %v", err)
	}
}

func TestValidateTopologyIllegalDiameter(t *testing.T) {
	snap := validSnapshot()
	// Single drain with a 110 -> 50 step not in the transition table.
	g := &topology.HydraulicGraph{
		Drains: []topology.RoofDrain{{ID: "D1", Zone: "Z1"}},
		Ports: []topology.Port{
			{ID: "P_D1", Owner: "D1", Diameter: 110},
			{ID: "P_S1", Owner: "D1_S", Diameter: 50},
			{ID: "P_O1", Owner: "O1", Diameter: 50},
		},
		Outlets: []topology.Outlet{{ID: "O1"}},
		Edges: []topology.DirectedEdge{
			{From: "P_D1", To: "P_S1"},
			{From: "P_S1", To: "P_O1"},
		},
	}
	err := snap.ValidateTopology(g)
	if err == nil {
		t.Fatal("expected illegal diameter rejection")
	}
	if se, ok := err.(*domain.StableError); !ok || se.Code != domain.CodeIllegalDiameter {
		t.Fatalf("expected CodeIllegalDiameter, got %v", err)
	}
}

func TestSortedReasonsStableOrder(t *testing.T) {
	got := catalog.SortedReasons([]string{"z", "a", "m"})
	want := []string{"a", "m", "z"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortedReasons = %v, want %v", got, want)
		}
	}
}
