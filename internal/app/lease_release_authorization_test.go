package app

import (
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func TestModel_ReleasedLeaseCannotAuthorizeServiceBoundary(t *testing.T) {
	tests := []struct {
		name     string
		boundary string
		release  lineage.ResourceType
		allowed  bool
	}{
		{name: "released welder rejects weld stage", boundary: "weld", release: lineage.ResourceWelder},
		{name: "released clamp rejects weld stage", boundary: "weld", release: lineage.ResourceClamp},
		{name: "active weld leases retain successful progression", boundary: "weld", allowed: true},
		{name: "released borescope rejects inspection", boundary: "inspection", release: lineage.ResourceBorescope},
		{name: "active borescope lease retains successful inspection", boundary: "inspection", allowed: true},
		{name: "released water zone rejects water test", boundary: "water", release: lineage.ResourceWaterZone},
		{name: "active water zone lease retains successful start", boundary: "water", allowed: true},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newTestService(t)
			taskID := domain.TaskID("lease-boundary-task-" + string(rune('a'+i)))
			lockTask(t, svc, taskID, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)

			switch tc.boundary {
			case "weld":
				leases := []LeaseRequest{
					{ResourceType: lineage.ResourceWelder, ResourceID: "welder-1", Holder: taskID, Start: 0, End: 100},
					{ResourceType: lineage.ResourceClamp, ResourceID: "clamp-1", Holder: taskID, Start: 0, End: 100},
				}
				for j, lease := range leases {
					if _, err := svc.AcquireLease(taskID, domain.OperationID("acquire-"+string(rune('a'+j))), CanonicalDigest(lease), lease); err != nil {
						t.Fatalf("acquire %s: %v", lease.ResourceType, err)
					}
					if !tc.allowed && lease.ResourceType == tc.release {
						if _, err := svc.ReleaseLease(taskID, domain.OperationID("release"), CanonicalDigest(lease), lease); err != nil {
							t.Fatalf("release %s: %v", lease.ResourceType, err)
						}
					}
				}

				before, _ := svc.GetTask(taskID)
				req := WeldStageRequest{
					Weld: "w1", Generation: 1, Stage: weld.StageTrimming,
					Machine: "welder-1", Clamp: "clamp-1", PortA: "drain-A-port", PortB: "out-A-port",
					Temperature: 20, Humidity: 50, LogicalTime: 10,
				}
				res, err := svc.SubmitWeldStage(taskID, domain.OperationID("submit"), CanonicalDigest(req), req)
				if tc.allowed {
					if err != nil || res.PrefixLen != 1 {
						t.Fatalf("active leases should authorize prefix_len=1, result=%+v err=%v", res, err)
					}
					return
				}
				stable, ok := err.(*domain.StableError)
				if !ok || stable.Code != domain.CodeLeaseConflict {
					t.Fatalf("expected stable LEASE_CONFLICT, got %T %v", err, err)
				}
				after, _ := svc.GetTask(taskID)
				if len(after.Attempts) != len(before.Attempts) || after.Task.EventSeq != before.Task.EventSeq {
					t.Fatalf("rejected stage caused side effects: attempts %d->%d events %d->%d", len(before.Attempts), len(after.Attempts), before.Task.EventSeq, after.Task.EventSeq)
				}
				if ev, ok := after.currentWeld("w1"); ok || len(ev.Prefix.Stages) != 0 {
					t.Fatalf("rejected stage advanced weld evidence: ok=%v prefix_len=%d", ok, len(ev.Prefix.Stages))
				}

			case "inspection":
				submitFullWeld(t, svc, taskID, "w1", "drain-A-port", "out-A-port")
				lease := LeaseRequest{ResourceType: lineage.ResourceBorescope, ResourceID: "borescope-w1", Holder: taskID, Start: 0, End: 100}
				if _, err := svc.AcquireLease(taskID, "acquire-borescope", CanonicalDigest(lease), lease); err != nil {
					t.Fatalf("acquire borescope: %v", err)
				}
				if !tc.allowed {
					if _, err := svc.ReleaseLease(taskID, "release-borescope", CanonicalDigest(lease), lease); err != nil {
						t.Fatalf("release borescope: %v", err)
					}
				}
				before, _ := svc.GetTask(taskID)
				req := InspectionRequest{Weld: "w1", Generation: 1, Appearance: "ok", Borescope: "ok", HangerOK: true, FixedNodeOK: true, LogicalTime: 20}
				res, err := svc.SubmitInspection(taskID, "inspect", CanonicalDigest(req), req)
				if tc.allowed {
					if err != nil || !res.Installed {
						t.Fatalf("active borescope lease should authorize inspection, result=%+v err=%v", res, err)
					}
					return
				}
				stable, ok := err.(*domain.StableError)
				if !ok || stable.Code != domain.CodeLeaseConflict {
					t.Fatalf("expected stable LEASE_CONFLICT, got %T %v", err, err)
				}
				after, _ := svc.GetTask(taskID)
				ev, _ := after.currentWeld("w1")
				if len(after.Attempts) != len(before.Attempts) || after.Task.EventSeq != before.Task.EventSeq || ev.Installed {
					t.Fatalf("rejected inspection caused side effects: attempts %d->%d events %d->%d installed=%v", len(before.Attempts), len(after.Attempts), before.Task.EventSeq, after.Task.EventSeq, ev.Installed)
				}

			case "water":
				lease := LeaseRequest{ResourceType: lineage.ResourceWaterZone, ResourceID: "water-zone-A", Holder: taskID, Start: 0, End: 100}
				if _, err := svc.AcquireLease(taskID, "acquire-water", CanonicalDigest(lease), lease); err != nil {
					t.Fatalf("acquire water zone: %v", err)
				}
				if !tc.allowed {
					if _, err := svc.ReleaseLease(taskID, "release-water", CanonicalDigest(lease), lease); err != nil {
						t.Fatalf("release water zone: %v", err)
					}
				}
				before, _ := svc.GetTask(taskID)
				res, err := svc.StartWaterTest(taskID, "start-water", "start-water-digest", "zone-A", 10)
				if tc.allowed {
					if err != nil || res.Zone != "zone-A" {
						t.Fatalf("active water-zone lease should authorize start, result=%+v err=%v", res, err)
					}
					return
				}
				stable, ok := err.(*domain.StableError)
				if !ok || stable.Code != domain.CodeLeaseConflict {
					t.Fatalf("expected stable LEASE_CONFLICT, got %T %v", err, err)
				}
				after, _ := svc.GetTask(taskID)
				if len(after.Attempts) != len(before.Attempts) || after.Task.EventSeq != before.Task.EventSeq || len(after.WaterTests) != len(before.WaterTests) {
					t.Fatalf("rejected water test caused side effects: attempts %d->%d events %d->%d sessions %d->%d", len(before.Attempts), len(after.Attempts), before.Task.EventSeq, after.Task.EventSeq, len(before.WaterTests), len(after.WaterTests))
				}
			}
		})
	}
}
