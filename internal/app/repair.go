package app

import (
	"fmt"
	"sort"

	"siphonic-roof-drainage-overflow-release/internal/domain"
)

// AnomalyRequest registers a detected defect.
type AnomalyRequest struct {
	Kind   AnomalyKind   `json:"kind"`
	Weld   domain.WeldID `json:"weld,omitempty"`
	Zone   domain.ZoneID `json:"zone,omitempty"`
	Detail string        `json:"detail"`
}

// RepairSetResult reports the deterministic repair set for an anomaly.
type RepairSetResult struct {
	RepairSet RepairSet `json:"repair_set"`
}

// RepairRequest cuts out the welds of a repair set and opens a new generation.
type RepairRequest struct {
	RepairSetID   string            `json:"repair_set_id"`
	NewGeneration domain.Generation `json:"new_generation"`
}

// RepairResult reports the welds that were cut out and re-opened.
type RepairResult struct {
	RepairSetID string            `json:"repair_set_id"`
	Generation  domain.Generation `json:"generation"`
	CutWelds    []domain.WeldID   `json:"cut_welds"`
}

// RegisterAnomaly records an anomaly and derives its deterministic, sorted,
// deduplicated cut-out / re-weld set from hydraulic connectivity, the shared
// heating-plate contamination window, material batch and common suspension
// branch.
func (s *Service) RegisterAnomaly(taskID domain.TaskID, opID domain.OperationID, digest string, req AnomalyRequest) (*RepairSetResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		if req.Kind == "" {
			return nil, domain.NewError(domain.CodeInvalidArgument, "anomaly kind is required")
		}
		id := fmt.Sprintf("anomaly-%d", len(st.Anomalies)+1)
		a := Anomaly{ID: id, Kind: req.Kind, Weld: req.Weld, Zone: req.Zone, Detail: req.Detail}
		st.Anomalies = append(st.Anomalies, a)

		items := s.repairScope(st, a)
		key := fmt.Sprintf("%s:%d", id, st.Task.Generation)
		set := RepairSet{ID: fmt.Sprintf("repair-%d", len(st.RepairSets)+1), Key: key, Items: items}
		st.RepairSets = append(st.RepairSets, set)
		st.appendEvent("ANOMALY", string(req.Kind))
		return jsonResult(RepairSetResult{RepairSet: set})
	})
	if err != nil {
		return nil, err
	}
	var out RepairSetResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Repair cuts out the welds of a repair set: the bound materials are marked
// removed (the cut-out segment disposition), a new generation is opened for
// each weld, and the old evidence is preserved for audit. The whole operation
// is atomic.
func (s *Service) Repair(taskID domain.TaskID, opID domain.OperationID, digest string, req RepairRequest) (*RepairResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		var set *RepairSet
		for i := range st.RepairSets {
			if st.RepairSets[i].ID == req.RepairSetID {
				set = &st.RepairSets[i]
			}
		}
		if set == nil {
			return nil, domain.NewError(domain.CodeNotFound, "unknown repair set "+req.RepairSetID)
		}
		if req.NewGeneration <= 0 {
			return nil, domain.NewError(domain.CodeInvalidArgument, "new generation must be positive")
		}
		var cut []domain.WeldID
		for _, item := range set.Items {
			ev, ok := st.currentWeld(item.Weld)
			if !ok {
				continue
			}
			if req.NewGeneration <= ev.Generation {
				return nil, domain.NewError(domain.CodeInvalidArgument, "new generation must exceed current")
			}
			// Record the cut-out segment disposition for the bound materials.
			for _, port := range []domain.PortID{ev.PortA, ev.PortB} {
				if mat, ok := st.PortBindings[port]; ok {
					st.Lineage.Dispositions[mat] = domain.DispositionRemoved
				}
			}
			// Open a new generation; the old evidence stays in the list.
			st.CurrentGen[item.Weld] = req.NewGeneration
			cut = append(cut, item.Weld)
			st.appendEvent("WELD_CUT", fmt.Sprintf("%s -> generation %d", item.Weld, req.NewGeneration))
		}
		return jsonResult(RepairResult{RepairSetID: req.RepairSetID, Generation: req.NewGeneration, CutWelds: cut})
	})
	if err != nil {
		return nil, err
	}
	var out RepairResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// repairScope derives the deterministic repair set for an anomaly.
func (s *Service) repairScope(st *TaskState, a Anomaly) []RepairItem {
	seen := make(map[string]RepairItem)
	add := func(w domain.WeldID, reason string) {
		gen, ok := st.CurrentGen[w]
		if !ok {
			return
		}
		key := fmt.Sprintf("%s:%d", w, gen)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = RepairItem{Weld: w, Generation: gen, Reason: reason}
	}

	switch a.Kind {
	case AnomalyContamination:
		ev, ok := st.currentWeld(a.Weld)
		machine := domain.ResourceID("")
		if ok {
			machine = ev.Machine
		}
		batch := s.weldBatch(st, a.Weld)
		for w := range st.CurrentGen {
			wev, _ := st.currentWeld(w)
			if machine != "" && wev.Machine == machine {
				add(w, "shared heating plate contamination window")
			}
			if batch != "" && s.weldBatch(st, w) == batch {
				add(w, "shared material batch")
			}
		}
	case AnomalyLeak, AnomalyDrainInsufficient:
		zone := a.Zone
		if zone == "" {
			zone = s.weldZone(st, a.Weld)
		}
		for w := range st.CurrentGen {
			if zone != "" && s.weldZone(st, w) == zone {
				add(w, "hydraulic connectivity in zone "+string(zone))
			}
		}
	case AnomalySupportShift:
		branches := s.weldSupportBranches(st, a.Weld)
		for w := range st.CurrentGen {
			if sharesAny(branches, s.weldSupportBranches(st, w)) {
				add(w, "common suspension branch")
			}
		}
	default:
		add(a.Weld, "direct weld defect")
	}

	items := make([]RepairItem, 0, len(seen))
	for _, it := range seen {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Weld != items[j].Weld {
			return items[i].Weld < items[j].Weld
		}
		return items[i].Generation < items[j].Generation
	})
	return items
}

// weldBatch returns the batch of the materials bound to a weld's ports.
func (s *Service) weldBatch(st *TaskState, weld domain.WeldID) domain.BatchID {
	ev, ok := st.currentWeld(weld)
	if !ok {
		return ""
	}
	for _, port := range []domain.PortID{ev.PortA, ev.PortB} {
		if mat, ok := st.PortBindings[port]; ok {
			if node, ok := st.Lineage.Nodes[mat]; ok {
				return node.Batch
			}
		}
	}
	return ""
}

// weldZone resolves the zone of a weld from the zone of the segment owning one
// of its ports.
func (s *Service) weldZone(st *TaskState, weld domain.WeldID) domain.ZoneID {
	ev, ok := st.currentWeld(weld)
	if !ok {
		return ""
	}
	for _, port := range []domain.PortID{ev.PortA, ev.PortB} {
		for _, seg := range st.Task.Graph.Segments {
			if seg.From == port || seg.To == port {
				return seg.Zone
			}
		}
	}
	return ""
}

// weldSupportBranches returns the support-branch ids whose segments own one of
// a weld's ports.
func (s *Service) weldSupportBranches(st *TaskState, weld domain.WeldID) []string {
	ev, ok := st.currentWeld(weld)
	if !ok {
		return nil
	}
	segments := map[domain.SegmentID]bool{}
	for _, port := range []domain.PortID{ev.PortA, ev.PortB} {
		for _, seg := range st.Task.Graph.Segments {
			if seg.From == port || seg.To == port {
				segments[seg.ID] = true
			}
		}
	}
	var out []string
	for _, br := range st.Task.Graph.SupportBranches {
		for _, sid := range br.Segments {
			if segments[sid] {
				out = append(out, br.ID)
			}
		}
	}
	return out
}

func sharesAny(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		if set[x] {
			return true
		}
	}
	return false
}
