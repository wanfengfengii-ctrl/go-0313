// Package topology models the append-only hydraulic connection graph: zones,
// roof drains, directed pipe segments, fittings, ports, outlets, support
// branches, hangers and fixed nodes, together with the current effective
// weld generation.
package topology

import "siphonic-roof-drainage-overflow-release/internal/domain"

// Zone is a catchment area of the roof drainage system.
type Zone struct {
	ID     domain.ZoneID   `json:"id"`
	Name   string          `json:"name"`
	Outlet domain.OutletID `json:"outlet"`
}

// RoofDrain is a roof drain (雨水斗) belonging to a zone.
type RoofDrain struct {
	ID   domain.DrainID `json:"id"`
	Zone domain.ZoneID  `json:"zone"`
}

// PipeSegment is a directed pipe run between two ports.
type PipeSegment struct {
	ID       domain.SegmentID  `json:"id"`
	Zone     domain.ZoneID     `json:"zone"`
	From     domain.PortID     `json:"from"`
	To       domain.PortID     `json:"to"`
	Diameter domain.DiameterMM `json:"diameter"`
	LengthMM domain.LengthMM   `json:"length_mm"`
}

// Fitting is a tee, elbow or reducer with one or more ports.
type Fitting struct {
	ID    domain.FittingID `json:"id"`
	Ports []domain.PortID  `json:"ports"`
}

// Port is a single connection point. A port belongs to at most one current
// effective weld at any time.
type Port struct {
	ID       domain.PortID     `json:"id"`
	Owner    string            `json:"owner"` // drain, segment or fitting identifier
	Diameter domain.DiameterMM `json:"diameter"`
}

// Outlet is the roof-level discharge mouth (出户口) a zone drains to.
type Outlet struct {
	ID domain.OutletID `json:"id"`
}

// SupportBranch groups segments sharing a common suspension hanger.
type SupportBranch struct {
	ID       string             `json:"id"`
	Segments []domain.SegmentID `json:"segments"`
}

// Hanger is a suspension point (支吊架).
type Hanger struct {
	ID       string             `json:"id"`
	Segments []domain.SegmentID `json:"segments"`
}

// FixedNode is an immovable anchoring point (固定节点).
type FixedNode struct {
	ID       string             `json:"id"`
	Segments []domain.SegmentID `json:"segments"`
}

// DirectedEdge is a single directional link in the graph.
type DirectedEdge struct {
	From domain.PortID `json:"from"`
	To   domain.PortID `json:"to"`
}

// WeldGeneration reference ties a weld to the task generation it currently
// belongs to. Older generations remain queryable but are never current.
type WeldGeneration struct {
	Weld       domain.WeldID     `json:"weld"`
	Generation domain.Generation `json:"generation"`
	Current    bool              `json:"current"`
}

// HydraulicGraph is the immutable-once-locked connection graph. After the
// task lock it is append-only: fields are never mutated in place.
type HydraulicGraph struct {
	Zones           []Zone           `json:"zones"`
	Drains          []RoofDrain      `json:"drains"`
	Segments        []PipeSegment    `json:"segments"`
	Fittings        []Fitting        `json:"fittings"`
	Ports           []Port           `json:"ports"`
	Outlets         []Outlet         `json:"outlets"`
	SupportBranches []SupportBranch  `json:"support_branches"`
	Hangers         []Hanger         `json:"hangers"`
	FixedNodes      []FixedNode      `json:"fixed_nodes"`
	Edges           []DirectedEdge   `json:"edges"`
	WeldGenerations []WeldGeneration `json:"weld_generations"`
}

// PortsByOwner returns the ports declared by the owner with the given id.
func (g *HydraulicGraph) PortsByOwner(owner string) []Port {
	var out []Port
	for _, p := range g.Ports {
		if p.Owner == owner {
			out = append(out, p)
		}
	}
	return out
}

// EdgeFrom returns the directed edge leaving the given port, or nil.
func (g *HydraulicGraph) EdgeFrom(from domain.PortID) *DirectedEdge {
	for i := range g.Edges {
		if g.Edges[i].From == from {
			return &g.Edges[i]
		}
	}
	return nil
}
