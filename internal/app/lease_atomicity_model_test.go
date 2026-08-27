package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
)

func TestModel_FailedLeaseRequestDoesNotPolluteTaskState(t *testing.T) {
	tests := []struct {
		name     string
		request  LeaseRequest
		wantCode domain.ErrorCode
	}{
		{
			name: "overlapping interval",
			request: LeaseRequest{
				ResourceType: lineage.ResourceWelder,
				ResourceID:   "welder-1",
				Holder:       "lease-atomicity",
				Start:        50,
				End:          200,
			},
			wantCode: domain.CodeLeaseConflict,
		},
		{
			name: "missing resource",
			request: LeaseRequest{
				ResourceType: lineage.ResourceWelder,
				Holder:       "lease-atomicity",
				Start:        100,
				End:          150,
			},
			wantCode: domain.CodeInvalidArgument,
		},
		{
			name: "degenerate interval",
			request: LeaseRequest{
				ResourceType: lineage.ResourceWelder,
				ResourceID:   "welder-1",
				Holder:       "lease-atomicity",
				Start:        150,
				End:          150,
			},
			wantCode: domain.CodeDegenerate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, persistentStore := newTestService(t)
			taskID := domain.TaskID("lease-atomicity")
			lockTask(t, svc, taskID, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)

			initial := LeaseRequest{
				ResourceType: lineage.ResourceWelder,
				ResourceID:   "welder-1",
				Holder:       taskID,
				Start:        0,
				End:          100,
			}
			if _, err := svc.AcquireLease(taskID, "initial-lease", CanonicalDigest(initial), initial); err != nil {
				t.Fatalf("acquire initial lease: %v", err)
			}

			before, ok := svc.GetTask(taskID)
			if !ok {
				t.Fatal("task missing before failed request")
			}
			beforeJSON, err := json.Marshal(before)
			if err != nil {
				t.Fatalf("marshal state before failed request: %v", err)
			}
			recoveredBefore, err := persistentStore.Recover()
			if err != nil {
				t.Fatalf("recover snapshot before failed request: %v", err)
			}
			snapshotBefore := recoveredBefore.Snapshots[string(taskID)]

			failedOp := domain.OperationID("failed-lease")
			_, err = svc.AcquireLease(taskID, failedOp, CanonicalDigest(tt.request), tt.request)
			stableErr, ok := err.(*domain.StableError)
			if !ok || stableErr.Code != tt.wantCode {
				t.Fatalf("failed request error = %v, want code %s", err, tt.wantCode)
			}

			after, ok := svc.GetTask(taskID)
			if !ok {
				t.Fatal("task missing after failed request")
			}
			afterJSON, err := json.Marshal(after)
			if err != nil {
				t.Fatalf("marshal state after failed request: %v", err)
			}
			if !bytes.Equal(afterJSON, beforeJSON) {
				t.Fatalf("failed request changed live task state\nbefore: %s\nafter:  %s", beforeJSON, afterJSON)
			}

			recoveredAfter, err := persistentStore.Recover()
			if err != nil {
				t.Fatalf("recover snapshot after failed request: %v", err)
			}
			if !bytes.Equal(recoveredAfter.Snapshots[string(taskID)], snapshotBefore) {
				t.Fatal("failed request changed the persisted task snapshot")
			}
			if _, ok := recoveredAfter.Operations[string(failedOp)]; ok {
				t.Fatal("failed request persisted an operation record")
			}

			adjacent := LeaseRequest{
				ResourceType: lineage.ResourceWelder,
				ResourceID:   "welder-1",
				Holder:       taskID,
				Start:        100,
				End:          150,
			}
			result, err := svc.AcquireLease(taskID, "adjacent-lease", CanonicalDigest(adjacent), adjacent)
			if err != nil {
				t.Fatalf("acquire adjacent non-overlapping lease after rejection: %v", err)
			}
			if result == nil || !result.Acquired {
				t.Fatalf("adjacent lease result = %#v, want acquired", result)
			}

			committed, ok := svc.GetTask(taskID)
			if !ok {
				t.Fatal("task missing after successful adjacent request")
			}
			if len(committed.Leases) != len(before.Leases)+1 || len(committed.Events) != len(before.Events)+1 {
				t.Fatalf("successful request wrote leases/events = %d/%d, want %d/%d",
					len(committed.Leases), len(committed.Events), len(before.Leases)+1, len(before.Events)+1)
			}
			if got := committed.Events[len(committed.Events)-1].Kind; got != "LEASE_ACQUIRED" {
				t.Fatalf("successful request event kind = %q, want LEASE_ACQUIRED", got)
			}

			recoveredSuccess, err := persistentStore.Recover()
			if err != nil {
				t.Fatalf("recover snapshot after successful request: %v", err)
			}
			committedJSON, err := json.Marshal(committed)
			if err != nil {
				t.Fatalf("marshal successful state: %v", err)
			}
			if !bytes.Equal(recoveredSuccess.Snapshots[string(taskID)], committedJSON) {
				t.Fatal("successful lease and audit event were not persisted atomically")
			}
		})
	}
}
