package app_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/app"
	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
	"siphonic-roof-drainage-overflow-release/internal/store"
	"siphonic-roof-drainage-overflow-release/internal/topology"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func TestModel_DeviceFailureAttemptsSurviveRestart(t *testing.T) {
	cases := []struct {
		name        string
		resultClass weld.ResultClass
	}{
		{name: "rejected", resultClass: weld.ResultRejected},
		{name: "disconnected", resultClass: weld.ResultDisconnect},
		{name: "timeout", resultClass: weld.ResultTimeout},
		{name: "calibration expired", resultClass: weld.ResultCalExpired},
		{name: "malformed", resultClass: weld.ResultMalformed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "recovery.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })

			adapter := &weld.ScriptedAdapter{DefaultOutcome: weld.ScriptOutcome{
				Reading: 8675309,
				Attempt: weld.DeviceAttempt{ResultClass: tc.resultClass},
			}}
			registry := &weld.ScriptedRegistry{Adapters: map[domain.ResourceID]weld.DeviceAdapter{
				"welder-recovery": adapter,
			}}
			newService := func() *app.Service {
				svc, err := app.NewService(db, catalog.DemoSnapshot(), registry)
				if err != nil {
					t.Fatalf("restart service: %v", err)
				}
				return svc
			}

			svc := newService()
			taskID := domain.TaskID("task-device-recovery")
			create := app.CreateTaskRequest{TaskID: taskID}
			if _, err := svc.CreateTask("create", app.CanonicalDigest(create), create); err != nil {
				t.Fatalf("create task: %v", err)
			}
			graph := topology.HydraulicGraph{
				Zones:    []topology.Zone{{ID: "zone-recovery", Name: "Recovery", Outlet: "out-recovery"}},
				Drains:   []topology.RoofDrain{{ID: "drain-recovery", Zone: "zone-recovery"}},
				Segments: []topology.PipeSegment{{ID: "segment-recovery", Zone: "zone-recovery", From: "drain-port", To: "out-port", Diameter: 110, LengthMM: 1000}},
				Ports: []topology.Port{
					{ID: "drain-port", Owner: "drain-recovery", Diameter: 110},
					{ID: "out-port", Owner: "out-recovery", Diameter: 110},
				},
				Outlets: []topology.Outlet{{ID: "out-recovery"}},
				Edges:   []topology.DirectedEdge{{From: "drain-port", To: "out-port"}},
			}
			lock := app.LockTaskRequest{TaskID: taskID, SummaryVersion: 1, Graph: graph}
			if _, err := svc.LockTask("lock", app.CanonicalDigest(lock), lock); err != nil {
				t.Fatalf("lock task: %v", err)
			}
			leases := []app.LeaseRequest{
				{ResourceType: lineage.ResourceWelder, ResourceID: "welder-recovery", Holder: taskID, Start: 0, End: 100},
				{ResourceType: lineage.ResourceClamp, ResourceID: "clamp-recovery", Holder: taskID, Start: 0, End: 100},
			}
			for i, lease := range leases {
				if _, err := svc.AcquireLease(taskID, domain.OperationID("lease-"+string(rune('0'+i))), app.CanonicalDigest(lease), lease); err != nil {
					t.Fatalf("acquire lease %d: %v", i, err)
				}
			}

			submit := func(svc *app.Service, op domain.OperationID, logicalTime int64) (*app.WeldStageResult, error) {
				req := app.WeldStageRequest{
					Weld: "weld-recovery", Generation: 1, Stage: weld.StageTrimming,
					Machine: "welder-recovery", Clamp: "clamp-recovery",
					PortA: "drain-port", PortB: "out-port", Temperature: 20, Humidity: 50,
					LogicalTime: logicalTime,
				}
				return svc.SubmitWeldStage(taskID, op, app.CanonicalDigest(req), req)
			}
			assertFailureState := func(label string, st *app.TaskState, expected []weld.DeviceAttempt) {
				t.Helper()
				if !reflect.DeepEqual(st.Attempts, expected) {
					t.Fatalf("%s attempts mismatch\n got: %#v\nwant: %#v", label, st.Attempts, expected)
				}
				if len(st.Welds["weld-recovery"]) != 0 {
					t.Fatalf("%s persisted weld evidence after failed device calls: %#v", label, st.Welds["weld-recovery"])
				}
			}

			expected := make([]weld.DeviceAttempt, 0, 2)
			for attemptIndex := 1; attemptIndex <= 2; attemptIndex++ {
				logicalTime := int64(attemptIndex)
				_, err := submit(svc, domain.OperationID("failure-"+string(rune('0'+attemptIndex))), logicalTime)
				var stable *domain.StableError
				if !errors.As(err, &stable) || stable.Code != domain.CodeDeviceFailure {
					t.Fatalf("attempt %d: expected DEVICE_FAILURE, got %v", attemptIndex, err)
				}
				expected = append(expected, weld.DeviceAttempt{
					DeviceType: "welder", ScriptKey: "weld:weld-recovery:stage:0", LogicalTime: logicalTime,
					Attempt: domain.AttemptIndex(attemptIndex), ResultClass: tc.resultClass,
					Reading: 0, Retryable: true, RetryLimit: catalog.DemoSnapshot().RetryLimit,
				})
				beforeRestart, ok := svc.GetTask(taskID)
				if !ok {
					t.Fatalf("attempt %d: task missing before restart", attemptIndex)
				}
				assertFailureState("before restart", beforeRestart, expected)

				svc = newService()
				afterRestart, ok := svc.GetTask(taskID)
				if !ok {
					t.Fatalf("attempt %d: task missing after restart", attemptIndex)
				}
				assertFailureState("after restart", afterRestart, expected)
			}

			adapter.DefaultOutcome = weld.ScriptOutcome{
				Reading: 42,
				Attempt: weld.DeviceAttempt{ResultClass: weld.ResultSuccess},
			}
			result, err := submit(svc, "eventual-success", 3)
			if err != nil {
				t.Fatalf("successful retry: %v", err)
			}
			if result.PrefixLen != 1 {
				t.Fatalf("successful retry prefix length = %d, want 1", result.PrefixLen)
			}
			if _, err := submit(svc, "eventual-success", 3); err != nil {
				t.Fatalf("idempotent replay: %v", err)
			}
			final, ok := svc.GetTask(taskID)
			if !ok {
				t.Fatal("task missing after successful retry")
			}
			if len(final.Attempts) != 3 {
				t.Fatalf("idempotent replay appended an attempt: got %d attempts, want 3", len(final.Attempts))
			}
			last := final.Attempts[2]
			if last.Attempt != 3 || last.ResultClass != weld.ResultSuccess || last.Reading != 42 || last.Retryable {
				t.Fatalf("successful attempt mismatch: %#v", last)
			}
			if got := len(final.Welds["weld-recovery"][0].Prefix.Stages); got != 1 {
				t.Fatalf("successful stage prefix length = %d, want 1", got)
			}
		})
	}
}
