package catalog

import (
	"sort"

	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/topology"
)

// ValidateTopology checks the graph invariants required before a task can be
// locked: port uniqueness, acyclicity, and a single continuous legal
// diameter path from every roof drain to an outlet. It returns the first
// stable error with deterministically ordered reasons.
func (r *RuleSnapshot) ValidateTopology(g *topology.HydraulicGraph) error {
	if err := validatePortsUnique(g); err != nil {
		return err
	}
	if err := validateAcyclic(g); err != nil {
		return err
	}
	return r.validateReachability(g)
}

// validatePortsUnique ensures no port id is declared more than once.
func validatePortsUnique(g *topology.HydraulicGraph) error {
	seen := make(map[domain.PortID]string, len(g.Ports))
	var reasons []string
	for _, p := range g.Ports {
		if prev, ok := seen[p.ID]; ok {
			reasons = append(reasons, "duplicate port "+string(p.ID)+" owned by "+prev+" and "+p.Owner)
			continue
		}
		seen[p.ID] = p.Owner
	}
	if len(reasons) > 0 {
		return domain.NewError(domain.CodeDuplicatePort, reasons...)
	}
	return nil
}

// validateAcyclic performs a deterministic depth-first search over directed
// edges and rejects any cycle.
func validateAcyclic(g *topology.HydraulicGraph) error {
	adj := make(map[domain.PortID][]domain.PortID)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	state := make(map[domain.PortID]uint8) // 0 unvisited, 1 visiting, 2 done
	var visit func(domain.PortID) []string
	visit = func(n domain.PortID) []string {
		state[n] = 1
		var cycle []string
		for _, m := range adj[n] {
			switch state[m] {
			case 1:
				cycle = append(cycle, "cycle reaches port "+string(m))
			case 0:
				if sub := visit(m); sub != nil {
					cycle = append(cycle, sub...)
				}
			}
		}
		state[n] = 2
		return cycle
	}
	for _, e := range g.Edges {
		if state[e.From] == 0 {
			if cycle := visit(e.From); len(cycle) > 0 {
				return domain.NewError(domain.CodeCycle, cycle...)
			}
		}
	}
	return nil
}

// validateReachability walks from every drain along the directed edges to an
// outlet, rejecting missing ports, forks, dead ends and illegal diameter
// transitions along the single required path.
func (r *RuleSnapshot) validateReachability(g *topology.HydraulicGraph) error {
	portDiameter := make(map[domain.PortID]domain.DiameterMM, len(g.Ports))
	outletOwner := make(map[string]bool)
	for _, o := range g.Outlets {
		outletOwner[string(o.ID)] = true
	}
	outlet := make(map[domain.PortID]bool)
	for _, p := range g.Ports {
		portDiameter[p.ID] = p.Diameter
		if outletOwner[p.Owner] {
			outlet[p.ID] = true
		}
	}
	adj := make(map[domain.PortID][]domain.PortID)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}

	for _, drain := range g.Drains {
		start := portOfDrain(drain.ID, g)
		if start == "" {
			return domain.NewError(domain.CodeDisconnected, "drain "+string(drain.ID)+" has no port")
		}
		if err := r.walkToOutlet(start, portDiameter, adj, outlet, nil); err != nil {
			return err
		}
	}
	return nil
}

// portOfDrain resolves the first port whose owner matches a drain id.
func portOfDrain(d domain.DrainID, g *topology.HydraulicGraph) domain.PortID {
	for _, p := range g.Ports {
		if p.Owner == string(d) {
			return p.ID
		}
	}
	return ""
}

func (r *RuleSnapshot) walkToOutlet(start domain.PortID, portDiameter map[domain.PortID]domain.DiameterMM, adj map[domain.PortID][]domain.PortID, outlet map[domain.PortID]bool, path []domain.PortID) error {
	for _, id := range path {
		if id == start {
			return domain.NewError(domain.CodeCycle, "cycle at port "+string(start))
		}
	}
	if outlet[start] {
		return nil
	}
	nexts := adj[start]
	if len(nexts) == 0 {
		return domain.NewError(domain.CodeDisconnected, "dead end at port "+string(start))
	}
	if len(nexts) > 1 {
		return domain.NewError(domain.CodeDisconnected, "fork at port "+string(start))
	}
	next := nexts[0]
	if !r.AllowsTransition(portDiameter[start], portDiameter[next]) {
		return domain.NewError(domain.CodeIllegalDiameter,
			"illegal diameter transition at port "+string(start))
	}
	return r.walkToOutlet(next, portDiameter, adj, outlet, append(path, start))
}

// SortedReasons returns a stable, lexicographically ordered copy of reasons.
func SortedReasons(reasons []string) []string {
	out := make([]string, len(reasons))
	copy(out, reasons)
	sort.Strings(out)
	return out
}
