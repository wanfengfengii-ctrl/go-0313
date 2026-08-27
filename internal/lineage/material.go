// Package lineage models the material pedigree and the mutex resource leases.
// Every conversion (cut, conversion, port binding) forms an append-only
// lineage edge, and the integer-millimetre conservation invariant is
// checkable at any point.
package lineage

import "siphonic-roof-drainage-overflow-release/internal/domain"

// MaterialIdentity is the unique identity of a lineage node.
type MaterialIdentity struct {
	ID     domain.MaterialID
	Batch  domain.BatchID
	Kind   domain.MaterialKind
	Length domain.LengthMM
}

// LineageEdge records an append-only conversion from a parent to a child.
type LineageEdge struct {
	Parent domain.MaterialID
	Child  domain.MaterialID
	Length domain.LengthMM // child length carved out of the parent
}

// MaterialLineage is the full pedigree for one task. Dispositions track where
// each node currently stands; the sum of a parent's descendants plus its own
// remaining length must equal the parent's original length.
type MaterialLineage struct {
	Nodes        map[domain.MaterialID]MaterialIdentity
	Edges        []LineageEdge
	Dispositions map[domain.MaterialID]domain.Disposition
}

// NewLineage returns an empty lineage.
func NewLineage() *MaterialLineage {
	return &MaterialLineage{
		Nodes:        make(map[domain.MaterialID]MaterialIdentity),
		Dispositions: make(map[domain.MaterialID]domain.Disposition),
	}
}

// AddNode registers a root material node. A duplicate id is rejected.
func (l *MaterialLineage) AddNode(n MaterialIdentity) error {
	if _, ok := l.Nodes[n.ID]; ok {
		return domain.NewError(domain.CodeInvalidArgument, "duplicate material id "+string(n.ID))
	}
	if n.Length < 0 {
		return domain.NewError(domain.CodeDegenerate, "negative length for "+string(n.ID))
	}
	l.Nodes[n.ID] = n
	l.Dispositions[n.ID] = domain.DispositionAvailable
	return nil
}

// Convert carves a child of the given length out of a parent, creating an
// append-only lineage edge and updating dispositions. It fails if the parent
// is unknown, already fully consumed, or the cut is degenerate.
func (l *MaterialLineage) Convert(parent, child domain.MaterialID, kind domain.MaterialKind, length domain.LengthMM) error {
	p, ok := l.Nodes[parent]
	if !ok {
		return domain.NewError(domain.CodeNotFound, "unknown parent "+string(parent))
	}
	if length <= 0 {
		return domain.NewError(domain.CodeDegenerate, "non-positive cut length")
	}
	remaining := l.Remaining(parent)
	if length > remaining {
		return domain.NewError(domain.CodeLineageBreach, "cut exceeds remaining length of "+string(parent))
	}
	l.Nodes[child] = MaterialIdentity{ID: child, Batch: p.Batch, Kind: kind, Length: length}
	l.Dispositions[child] = domain.DispositionAvailable
	l.Edges = append(l.Edges, LineageEdge{Parent: parent, Child: child, Length: length})
	return nil
}

// Remaining returns the unconsumed millimetres of a node: its own length
// minus the sum of its direct descendants.
func (l *MaterialLineage) Remaining(id domain.MaterialID) domain.LengthMM {
	n, ok := l.Nodes[id]
	if !ok {
		return 0
	}
	used := int64(0)
	for _, e := range l.Edges {
		if e.Parent == id {
			used += int64(e.Length)
		}
	}
	return n.Length - domain.LengthMM(used)
}

// Conserved reports whether every node's original length equals its remaining
// length plus the sum of its descendants' carved lengths (reasonable loss
// included). It returns the first offending node id when violated.
func (l *MaterialLineage) Conserved() (bool, domain.MaterialID) {
	for id, n := range l.Nodes {
		used := int64(0)
		for _, e := range l.Edges {
			if e.Parent == id {
				used += int64(e.Length)
			}
		}
		if used > int64(n.Length) {
			return false, id
		}
	}
	return true, ""
}
