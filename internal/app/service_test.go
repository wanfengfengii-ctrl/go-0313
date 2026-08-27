package app

import (
	"path/filepath"
	"sync"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/arbitration"
	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
	"siphonic-roof-drainage-overflow-release/internal/store"
	"siphonic-roof-drainage-overflow-release/internal/topology"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newTestService(t *testing.T) (*Service, store.Store) {
	t.Helper()
	st := newTestStore(t)
	svc, err := NewService(st, catalog.DemoSnapshot(),
		weld.PassThroughRegistry("welder-1", "clamp-1", "borescope-w1", "gauge-zone-A", "flow-zone-A"))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc, st
}

// validGraph returns a fixed, valid single-zone network: one drain, one
// segment and one outlet, all diameter 110.
func validGraph(zone, drain, seg, outlet domain.TaskID) topology.HydraulicGraph {
	return topology.HydraulicGraph{
		Zones:  []topology.Zone{{ID: domain.ZoneID(zone), Name: "Zone", Outlet: domain.OutletID(outlet)}},
		Drains: []topology.RoofDrain{{ID: domain.DrainID(drain), Zone: domain.ZoneID(zone)}},
		Segments: []topology.PipeSegment{{
			ID: domain.SegmentID(seg), Zone: domain.ZoneID(zone),
			From: domain.PortID(drain + "-port"), To: domain.PortID(outlet + "-port"),
			Diameter: 110, LengthMM: 1000,
		}},
		Ports: []topology.Port{
			{ID: domain.PortID(drain + "-port"), Owner: string(drain), Diameter: 110},
			{ID: domain.PortID(outlet + "-port"), Owner: string(outlet), Diameter: 110},
		},
		Outlets: []topology.Outlet{{ID: domain.OutletID(outlet)}},
		Edges:   []topology.DirectedEdge{{From: domain.PortID(drain + "-port"), To: domain.PortID(outlet + "-port")}},
	}
}

func lockTask(t *testing.T, svc *Service, id domain.TaskID, graph topology.HydraulicGraph, mats []MaterialSpec) {
	t.Helper()
	if _, err := svc.CreateTask(domain.OperationID("create-"+id), CanonicalDigest(CreateTaskRequest{TaskID: id}), CreateTaskRequest{TaskID: id}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	_, err := svc.LockTask(domain.OperationID("lock-"+id), CanonicalDigest(LockTaskRequest{TaskID: id, SummaryVersion: 1, Graph: graph, Materials: mats}),
		LockTaskRequest{TaskID: id, SummaryVersion: 1, Graph: graph, Materials: mats})
	if err != nil {
		t.Fatalf("lock task: %v", err)
	}
}

func TestLockTaskValid(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-1")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"),
		[]MaterialSpec{{ID: "pipe-1", Batch: "batch-1", Kind: domain.KindPipe, Length: 1000}})
	st, ok := svc.GetTask(id)
	if !ok {
		t.Fatal("task not found after lock")
	}
	if st.Task.LockState != topology.LockStateLocked {
		t.Fatalf("expected locked, got %s", st.Task.LockState)
	}
}

func TestLockTaskStaleSummary(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-2")
	if _, err := svc.CreateTask(domain.OperationID("c2"), "d", CreateTaskRequest{TaskID: id}); err != nil {
		t.Fatal(err)
	}
	req := LockTaskRequest{TaskID: id, SummaryVersion: 0, Graph: validGraph("zone-A", "drain-A", "seg-A", "out-A")}
	_, err := svc.LockTask(domain.OperationID("l2"), CanonicalDigest(req), req)
	if err == nil || err.(*domain.StableError).Code != domain.CodeStaleSummary {
		t.Fatalf("expected STALE_SUMMARY, got %v", err)
	}
}

func TestLockTaskCycle(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-3")
	g := validGraph("zone-A", "drain-A", "seg-A", "out-A")
	// Add a reverse edge creating a two-node cycle.
	g.Edges = append(g.Edges, topology.DirectedEdge{From: domain.PortID("out-A-port"), To: domain.PortID("drain-A-port")})
	if _, err := svc.CreateTask(domain.OperationID("c3"), "d", CreateTaskRequest{TaskID: id}); err != nil {
		t.Fatal(err)
	}
	req := LockTaskRequest{TaskID: id, SummaryVersion: 1, Graph: g}
	_, err := svc.LockTask(domain.OperationID("l3"), CanonicalDigest(req), req)
	if err == nil || err.(*domain.StableError).Code != domain.CodeCycle {
		t.Fatalf("expected CYCLE, got %v", err)
	}
}

func TestMaterialCutConservation(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-4")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"),
		[]MaterialSpec{{ID: "pipe-1", Batch: "batch-1", Kind: domain.KindPipe, Length: 1000}})

	cut := func(op string, req MaterialOpRequest) {
		t.Helper()
		if _, err := svc.MaterialOp(id, domain.OperationID(op), CanonicalDigest(req), req); err != nil {
			t.Fatalf("material op %s: %v", op, err)
		}
	}
	cut("m1", MaterialOpRequest{Parent: "pipe-1", Child: "stub-1", Kind: domain.KindStub, Length: 500, BindPort: "drain-A-port"})
	cut("m2", MaterialOpRequest{Parent: "pipe-1", Child: "sample-1", Kind: domain.KindSample, Length: 100})
	cut("m3", MaterialOpRequest{Parent: "pipe-1", Child: "loss-1", Kind: domain.KindLoss, Length: 50})

	st, _ := svc.GetTask(id)
	if rem := st.Lineage.Remaining("pipe-1"); rem != 350 {
		t.Fatalf("expected remaining 350, got %d", rem)
	}
	if ok, bad := st.Lineage.Conserved(); !ok {
		t.Fatalf("lineage not conserved at %s", bad)
	}
	if st.PortBindings["drain-A-port"] != "stub-1" {
		t.Fatalf("expected port bound to stub-1")
	}
}

func TestConcurrentMaterialCut(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-5")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"),
		[]MaterialSpec{{ID: "pipe-1", Batch: "batch-1", Kind: domain.KindPipe, Length: 1000}})

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, 2)
	req := MaterialOpRequest{Parent: "pipe-1", Child: "stub-x", Kind: domain.KindStub, Length: 600, BindPort: "drain-A-port"}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = svc.MaterialOp(id, domain.OperationID("cc-"+string(rune('a'+i))), CanonicalDigest(req), req)
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one success, got %d", successes)
	}
	st, _ := svc.GetTask(id)
	if len(st.Lineage.Edges) != 1 {
		t.Fatalf("expected one lineage edge, got %d", len(st.Lineage.Edges))
	}
}

func TestLeaseConflict(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-6")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)

	l1 := LeaseRequest{ResourceType: lineage.ResourceWelder, ResourceID: "welder-1", Holder: id, Start: 0, End: 100}
	if _, err := svc.AcquireLease(id, domain.OperationID("l1"), CanonicalDigest(l1), l1); err != nil {
		t.Fatal(err)
	}
	l2 := LeaseRequest{ResourceType: lineage.ResourceWelder, ResourceID: "welder-1", Holder: id, Start: 50, End: 200}
	if _, err := svc.AcquireLease(id, domain.OperationID("l2"), CanonicalDigest(l2), l2); err == nil {
		t.Fatal("expected lease conflict, got nil")
	}
}

func submitFullWeld(t *testing.T, svc *Service, id domain.TaskID, w domain.WeldID, portA, portB domain.PortID) {
	t.Helper()
	// Leases for welder and clamp.
	svc.AcquireLease(id, domain.OperationID("lease-w"), "d", LeaseRequest{ResourceType: lineage.ResourceWelder, ResourceID: "welder-1", Holder: id, Start: 0, End: 100000})
	svc.AcquireLease(id, domain.OperationID("lease-c"), "d", LeaseRequest{ResourceType: lineage.ResourceClamp, ResourceID: "clamp-1", Holder: id, Start: 0, End: 100000})

	stages := []weld.Stage{weld.StageTrimming, weld.StageFacing, weld.StageCleaning, weld.StageAlignment, weld.StageHeating, weld.StageSwitchover, weld.StagePressurize, weld.StageHold, weld.StageCooling}
	for i, stg := range stages {
		req := WeldStageRequest{
			Weld: w, Generation: 1, Stage: stg, Machine: "welder-1", Clamp: "clamp-1",
			PortA: portA, PortB: portB, Temperature: 20, Humidity: 50,
			BeadMM: 10, SwitchoverMS: 100, Pressures: []int64{100, 200}, CoolingMS: 200,
			LogicalTime: int64(i + 1),
		}
		if _, err := svc.SubmitWeldStage(id, domain.OperationID("w"+string(rune('a'+i))), CanonicalDigest(req), req); err != nil {
			t.Fatalf("stage %d: %v", i, err)
		}
	}
}

func TestWeldFullProgression(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-7")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)
	submitFullWeld(t, svc, id, "w1", "drain-A-port", "out-A-port")

	st, _ := svc.GetTask(id)
	ev, ok := st.currentWeld("w1")
	if !ok || !ev.Valid {
		t.Fatalf("expected valid weld, ok=%v valid=%v", ok, ev.Valid)
	}
	if len(ev.Prefix.Stages) != 9 {
		t.Fatalf("expected 9 stages, got %d", len(ev.Prefix.Stages))
	}
}

func TestWeldSkipStage(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-8")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)
	svc.AcquireLease(id, domain.OperationID("lw"), "d", LeaseRequest{ResourceType: lineage.ResourceWelder, ResourceID: "welder-1", Holder: id, Start: 0, End: 100000})
	svc.AcquireLease(id, domain.OperationID("lc"), "d", LeaseRequest{ResourceType: lineage.ResourceClamp, ResourceID: "clamp-1", Holder: id, Start: 0, End: 100000})

	// Try to submit the second stage first.
	req := WeldStageRequest{Weld: "w1", Generation: 1, Stage: weld.StageFacing, Machine: "welder-1", Clamp: "clamp-1", PortA: "drain-A-port", PortB: "out-A-port", Temperature: 20, Humidity: 50, LogicalTime: 1}
	_, err := svc.SubmitWeldStage(id, domain.OperationID("skip"), CanonicalDigest(req), req)
	if err == nil || err.(*domain.StableError).Code != domain.CodeStageOutOfOrder {
		t.Fatalf("expected STAGE_OUT_OF_ORDER, got %v", err)
	}
	st, _ := svc.GetTask(id)
	if ev, _ := st.currentWeld("w1"); len(ev.Prefix.Stages) != 0 {
		t.Fatalf("expected empty prefix, got %d", len(ev.Prefix.Stages))
	}
}

func TestDeviceFailureNoAdvance(t *testing.T) {
	svc, _ := newTestService(t)
	// Replace the device registry with one that rejects the welder.
	svc.devices = &weld.ScriptedRegistry{Adapters: map[domain.ResourceID]weld.DeviceAdapter{
		"welder-1": &weld.ScriptedAdapter{DefaultOutcome: weld.ScriptOutcome{
			Attempt: weld.DeviceAttempt{ResultClass: weld.ResultRejected},
		}},
	}}
	id := domain.TaskID("task-9")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)
	svc.AcquireLease(id, domain.OperationID("lw"), "d", LeaseRequest{ResourceType: lineage.ResourceWelder, ResourceID: "welder-1", Holder: id, Start: 0, End: 100000})
	svc.AcquireLease(id, domain.OperationID("lc"), "d", LeaseRequest{ResourceType: lineage.ResourceClamp, ResourceID: "clamp-1", Holder: id, Start: 0, End: 100000})

	req := WeldStageRequest{Weld: "w1", Generation: 1, Stage: weld.StageTrimming, Machine: "welder-1", Clamp: "clamp-1", PortA: "drain-A-port", PortB: "out-A-port", Temperature: 20, Humidity: 50, LogicalTime: 1}
	if _, err := svc.SubmitWeldStage(id, domain.OperationID("df"), CanonicalDigest(req), req); err == nil {
		t.Fatal("expected device failure")
	}
	st, _ := svc.GetTask(id)
	if len(st.Attempts) != 1 || st.Attempts[0].ResultClass != weld.ResultRejected {
		t.Fatalf("expected one rejected attempt, got %+v", st.Attempts)
	}
	if ev, _ := st.currentWeld("w1"); len(ev.Prefix.Stages) != 0 {
		t.Fatal("device failure must not advance prefix")
	}
}

func TestWaterTestBarrier(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-10")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)
	svc.AcquireLease(id, domain.OperationID("lz"), "d", LeaseRequest{ResourceType: lineage.ResourceWaterZone, ResourceID: "water-zone-A", Holder: id, Start: 0, End: 100000})

	if _, err := svc.StartWaterTest(id, domain.OperationID("wt0"), "d", "zone-A", 0); err != nil {
		t.Fatal(err)
	}
	adv := func(op string, req WaterTestRequest) *WaterTestResult {
		t.Helper()
		res, err := svc.AdvanceWaterTest(id, domain.OperationID(op), CanonicalDigest(req), req)
		if err != nil {
			t.Fatalf("advance %s: %v", op, err)
		}
		return res
	}
	adv("wt1", WaterTestRequest{Zone: "zone-A", Phase: arbitration.WaterPhaseFill, Value: 500, LogicalTime: 10})
	adv("wt2", WaterTestRequest{Zone: "zone-A", Phase: arbitration.WaterPhaseHold, LogicalTime: 2000})
	adv("wt3", WaterTestRequest{Zone: "zone-A", Phase: arbitration.WaterPhaseDrain, LogicalTime: 3000, DrainDurationMS: 1000})
	res := adv("wt4", WaterTestRequest{Zone: "zone-A", Phase: arbitration.WaterPhaseEmpty, LogicalTime: 4000})

	if !res.BarrierOpen {
		t.Fatal("expected barrier open after full water test")
	}
}

func TestRepairNewGeneration(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-11")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"),
		[]MaterialSpec{{ID: "pipe-1", Batch: "batch-1", Kind: domain.KindPipe, Length: 1000}})
	svc.MaterialOp(id, domain.OperationID("mb"), "d", MaterialOpRequest{Parent: "pipe-1", Child: "stub-1", Kind: domain.KindStub, Length: 500, BindPort: "drain-A-port"})
	submitFullWeld(t, svc, id, "w1", "drain-A-port", "out-A-port")

	anom := AnomalyRequest{Kind: AnomalyNecking, Weld: "w1", Detail: "inner wall necking"}
	ares, err := svc.RegisterAnomaly(id, domain.OperationID("an"), CanonicalDigest(anom), anom)
	if err != nil {
		t.Fatal(err)
	}
	if len(ares.RepairSet.Items) != 1 {
		t.Fatalf("expected one repair item, got %d", len(ares.RepairSet.Items))
	}
	rreq := RepairRequest{RepairSetID: ares.RepairSet.ID, NewGeneration: 2}
	if _, err := svc.Repair(id, domain.OperationID("rp"), CanonicalDigest(rreq), rreq); err != nil {
		t.Fatal(err)
	}
	st, _ := svc.GetTask(id)
	if st.CurrentGen["w1"] != 2 {
		t.Fatalf("expected new generation 2, got %d", st.CurrentGen["w1"])
	}
	// Old generation evidence preserved.
	if len(st.Welds["w1"]) != 1 {
		t.Fatalf("expected old evidence preserved")
	}
	if st.Lineage.Dispositions["stub-1"] != domain.DispositionRemoved {
		t.Fatalf("expected removed disposition, got %s", st.Lineage.Dispositions["stub-1"])
	}
}

func TestIdempotencyReplayAndConflict(t *testing.T) {
	svc, _ := newTestService(t)
	id := domain.TaskID("task-12")
	req := CreateTaskRequest{TaskID: id}
	op := domain.OperationID("op-1")
	digest := CanonicalDigest(req)
	if _, err := svc.CreateTask(op, digest, req); err != nil {
		t.Fatal(err)
	}
	// Replay with same content returns success (no error).
	if _, err := svc.CreateTask(op, digest, req); err != nil {
		t.Fatalf("replay should succeed, got %v", err)
	}
	// Different content conflicts.
	if _, err := svc.CreateTask(op, "different-digest", req); err == nil || err.(*domain.StableError).Code != domain.CodeIdempotencyConflict {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestRecoveryAfterRestart(t *testing.T) {
	svc, st := newTestService(t)
	id := domain.TaskID("task-13")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"),
		[]MaterialSpec{{ID: "pipe-1", Batch: "batch-1", Kind: domain.KindPipe, Length: 1000}})

	// Reopen the same store in a new service.
	svc2, err := NewService(st, catalog.DemoSnapshot(), weld.PassThroughRegistry("welder-1"))
	if err != nil {
		t.Fatal(err)
	}
	st2, ok := svc2.GetTask(id)
	if !ok || st2.Task.LockState != topology.LockStateLocked {
		t.Fatalf("state not recovered: ok=%v", ok)
	}
}

func TestRollbackOnCommitFailure(t *testing.T) {
	svc, st := newTestService(t)
	id := domain.TaskID("task-14")
	if _, err := svc.CreateTask(domain.OperationID("c14"), "d", CreateTaskRequest{TaskID: id}); err != nil {
		t.Fatal(err)
	}
	// Inject a commit failure.
	svc.FailBeforeCommit = func() error { return domain.NewError(domain.CodeInternal, "injected") }
	req := LockTaskRequest{TaskID: id, SummaryVersion: 1, Graph: validGraph("zone-A", "drain-A", "seg-A", "out-A")}
	if _, err := svc.LockTask(domain.OperationID("l14"), CanonicalDigest(req), req); err == nil {
		t.Fatal("expected injected commit failure")
	}
	svc.FailBeforeCommit = nil

	// Restart from store: the lock must not be present.
	svc2, err := NewService(st, catalog.DemoSnapshot(), weld.PassThroughRegistry("welder-1"))
	if err != nil {
		t.Fatal(err)
	}
	st2, _ := svc2.GetTask(id)
	if st2.Task.LockState == topology.LockStateLocked {
		t.Fatal("partial lock leaked through failed commit")
	}
}

func TestReviewAndFinalDecision(t *testing.T) {
	svc, _ := newTestService(t)
	svc.SetReviewerDirectory(map[string]ReviewerEntry{
		"a": {Qualified: true, QualExpiry: 1 << 60},
		"b": {Qualified: true, QualExpiry: 1 << 60},
		"x": {Qualified: false, QualExpiry: 1 << 60},
	})
	id := domain.TaskID("task-15")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)

	// Unqualified reviewer rejected.
	r1 := ReviewRequest{Reviewer: "x", Signature: "sig", LogicalTime: 1}
	if _, err := svc.SubmitReview(id, domain.OperationID("r0"), CanonicalDigest(r1), r1); err == nil {
		t.Fatal("expected unqualified reviewer rejection")
	}
	// Two distinct qualified reviewers.
	r2 := ReviewRequest{Reviewer: "a", Signature: "sig-a", LogicalTime: 1}
	if _, err := svc.SubmitReview(id, domain.OperationID("r1"), CanonicalDigest(r2), r2); err != nil {
		t.Fatal(err)
	}
	r3 := ReviewRequest{Reviewer: "b", Signature: "sig-b", LogicalTime: 2}
	if _, err := svc.SubmitReview(id, domain.OperationID("r2"), CanonicalDigest(r3), r3); err != nil {
		t.Fatal(err)
	}

	fd := FinalDecisionRequest{Type: arbitration.FinalAdmission, Credential: "cred-1"}
	res, err := svc.FinalDecision(id, domain.OperationID("fd"), CanonicalDigest(fd), fd)
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != arbitration.FinalAdmission || res.Credential == "" {
		t.Fatalf("expected admission with credential, got %+v", res)
	}
	// Second decision conflicts.
	fd2 := FinalDecisionRequest{Type: arbitration.FinalIsolation}
	if _, err := svc.FinalDecision(id, domain.OperationID("fd2"), CanonicalDigest(fd2), fd2); err == nil {
		t.Fatal("expected final decision conflict")
	}
}

func TestFinalDecisionConcurrentSingleWinner(t *testing.T) {
	svc, _ := newTestService(t)
	svc.SetReviewerDirectory(map[string]ReviewerEntry{
		"a": {Qualified: true, QualExpiry: 1 << 60},
		"b": {Qualified: true, QualExpiry: 1 << 60},
	})
	id := domain.TaskID("task-16")
	lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)
	svc.SubmitReview(id, domain.OperationID("r1"), "d", ReviewRequest{Reviewer: "a", Signature: "s", LogicalTime: 1})
	svc.SubmitReview(id, domain.OperationID("r2"), "d", ReviewRequest{Reviewer: "b", Signature: "s", LogicalTime: 2})

	types := []arbitration.FinalType{arbitration.FinalAdmission, arbitration.FinalIsolation, arbitration.FinalCancelled}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req := FinalDecisionRequest{Type: types[i], Credential: "cred"}
			_, errs[i] = svc.FinalDecision(id, domain.OperationID("fd"+string(rune('a'+i))), CanonicalDigest(req), req)
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one final decision winner, got %d", wins)
	}
	st, _ := svc.GetTask(id)
	if st.Final == nil {
		t.Fatal("expected a committed final decision")
	}
}
