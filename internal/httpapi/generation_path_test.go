package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/app"
	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/httpapi"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
	"siphonic-roof-drainage-overflow-release/internal/store"
	"siphonic-roof-drainage-overflow-release/internal/topology"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func TestModel_WeldGenerationPathValidation(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	svc, err := app.NewService(db, catalog.DemoSnapshot(),
		weld.PassThroughRegistry("welder-1", "clamp-1"))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	taskID := domain.TaskID("generation-path-task")
	create := app.CreateTaskRequest{TaskID: taskID}
	if _, err := svc.CreateTask("setup-create", app.CanonicalDigest(create), create); err != nil {
		t.Fatalf("create task: %v", err)
	}
	graph := topology.HydraulicGraph{
		Zones:    []topology.Zone{{ID: "zone-A", Name: "Zone A", Outlet: "out-A"}},
		Drains:   []topology.RoofDrain{{ID: "drain-A", Zone: "zone-A"}},
		Outlets:  []topology.Outlet{{ID: "out-A"}},
		Segments: []topology.PipeSegment{{ID: "seg-A", Zone: "zone-A", From: "drain-port", To: "out-port", Diameter: 110, LengthMM: 1000}},
		Ports: []topology.Port{
			{ID: "drain-port", Owner: "drain-A", Diameter: 110},
			{ID: "out-port", Owner: "out-A", Diameter: 110},
		},
		Edges: []topology.DirectedEdge{{From: "drain-port", To: "out-port"}},
	}
	lock := app.LockTaskRequest{TaskID: taskID, SummaryVersion: 1, Graph: graph}
	if _, err := svc.LockTask("setup-lock", app.CanonicalDigest(lock), lock); err != nil {
		t.Fatalf("lock task: %v", err)
	}
	for _, lease := range []app.LeaseRequest{
		{ResourceType: lineage.ResourceWelder, ResourceID: "welder-1", Holder: taskID, Start: 0, End: 100},
		{ResourceType: lineage.ResourceClamp, ResourceID: "clamp-1", Holder: taskID, Start: 0, End: 100},
	} {
		op := domain.OperationID("setup-lease-" + string(lease.ResourceID))
		if _, err := svc.AcquireLease(taskID, op, app.CanonicalDigest(lease), lease); err != nil {
			t.Fatalf("acquire %s: %v", lease.ResourceID, err)
		}
	}

	handler := httpapi.NewServer(svc).Handler()
	payload, err := json.Marshal(app.WeldStageRequest{
		Stage: weld.StageTrimming, Machine: "welder-1", Clamp: "clamp-1",
		PortA: "drain-port", PortB: "out-port", Temperature: 20, Humidity: 50,
		LogicalTime: 1,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	initial, ok := svc.GetTask(taskID)
	if !ok {
		t.Fatal("task missing after setup")
	}

	tests := []struct {
		name       string
		generation string
		wantStatus int
		malformed  bool
	}{
		{name: "non-numeric", generation: "not-a-number", wantStatus: http.StatusBadRequest, malformed: true},
		{name: "positive-overflow", generation: "9223372036854775808", wantStatus: http.StatusBadRequest, malformed: true},
		{name: "negative-overflow", generation: "-9223372036854775809", wantStatus: http.StatusBadRequest, malformed: true},
		{name: "zero", generation: "0", wantStatus: http.StatusBadRequest, malformed: true},
		{name: "negative", generation: "-1", wantStatus: http.StatusBadRequest, malformed: true},
		{name: "valid-and-idempotent", generation: "1", wantStatus: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opID := "stage-" + tc.name
			send := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost,
					"/v1/tasks/"+string(taskID)+"/welds/w1/generations/"+tc.generation+"/stages",
					bytes.NewReader(payload))
				req.Header.Set("Operation-Id", opID)
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)
				return rr
			}

			first, second := send(), send()
			if first.Code != tc.wantStatus || second.Code != tc.wantStatus {
				t.Fatalf("status first=%d second=%d, want %d; first body=%s", first.Code, second.Code, tc.wantStatus, first.Body.String())
			}
			if first.Body.String() != second.Body.String() {
				t.Fatalf("retry response changed: first=%q second=%q", first.Body.String(), second.Body.String())
			}

			state, ok := svc.GetTask(taskID)
			if !ok {
				t.Fatal("task disappeared")
			}
			recovered, err := db.Recover()
			if err != nil {
				t.Fatalf("recover store: %v", err)
			}
			_, operationRecorded := recovered.Operations[opID]

			if tc.malformed {
				var got httpapi.ErrorResponse
				if err := json.Unmarshal(first.Body.Bytes(), &got); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if got.Code != domain.CodeInvalidArgument || got.Message != "INVALID_ARGUMENT: generation path segment must be an integer" {
					t.Fatalf("unstable path error: code=%q message=%q", got.Code, got.Message)
				}
				if operationRecorded {
					t.Fatal("malformed path created an operation record")
				}
				if len(state.CurrentGen) != len(initial.CurrentGen) || len(state.Welds) != len(initial.Welds) ||
					len(state.Attempts) != len(initial.Attempts) || len(state.Events) != len(initial.Events) {
					t.Fatalf("malformed path mutated weld state: current=%v welds=%v attempts=%d events=%d",
						state.CurrentGen, state.Welds, len(state.Attempts), len(state.Events))
				}
				return
			}

			if !operationRecorded {
				t.Fatal("valid request did not create its idempotency record")
			}
			if state.CurrentGen["w1"] != 1 || len(state.Welds["w1"]) != 1 ||
				len(state.Welds["w1"][0].Prefix.Stages) != 1 {
				t.Fatalf("valid generation did not advance exactly one stage: current=%d welds=%+v",
					state.CurrentGen["w1"], state.Welds["w1"])
			}
			if len(state.Attempts) != len(initial.Attempts)+1 || len(state.Events) != len(initial.Events)+1 {
				t.Fatalf("idempotent retry duplicated effects: attempts=%d events=%d", len(state.Attempts), len(state.Events))
			}
		})
	}
}
