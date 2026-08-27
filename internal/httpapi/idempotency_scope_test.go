package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/app"
	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/httpapi"
	"siphonic-roof-drainage-overflow-release/internal/store"
	"siphonic-roof-drainage-overflow-release/internal/topology"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func TestModel_MaterialOperationIdempotencyIsScopedToTaskAndCanonicalBody(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "idempotency.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	svc, err := app.NewService(db, catalog.DemoSnapshot(), weld.PassThroughRegistry())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	graph := topology.HydraulicGraph{
		Zones:  []topology.Zone{{ID: "zone-A", Name: "Zone A", Outlet: "outlet-A"}},
		Drains: []topology.RoofDrain{{ID: "drain-A", Zone: "zone-A"}},
		Segments: []topology.PipeSegment{{
			ID: "segment-A", Zone: "zone-A", From: "drain-port", To: "outlet-port",
			Diameter: 110, LengthMM: 1000,
		}},
		Ports: []topology.Port{
			{ID: "drain-port", Owner: "drain-A", Diameter: 110},
			{ID: "outlet-port", Owner: "outlet-A", Diameter: 110},
		},
		Outlets: []topology.Outlet{{ID: "outlet-A"}},
		Edges:   []topology.DirectedEdge{{From: "drain-port", To: "outlet-port"}},
	}

	for _, taskID := range []domain.TaskID{"task-A", "task-B"} {
		create := app.CreateTaskRequest{TaskID: taskID}
		if _, err := svc.CreateTask(domain.OperationID("create-"+taskID), app.CanonicalDigest(create), create); err != nil {
			t.Fatalf("create %s: %v", taskID, err)
		}
		lock := app.LockTaskRequest{
			TaskID: taskID, SummaryVersion: 1, Graph: graph,
			Materials: []app.MaterialSpec{{ID: "pipe-1", Batch: "batch-1", Kind: domain.KindPipe, Length: 1000}},
		}
		if _, err := svc.LockTask(domain.OperationID("lock-"+taskID), app.CanonicalDigest(lock), lock); err != nil {
			t.Fatalf("lock %s: %v", taskID, err)
		}
	}

	handler := httpapi.NewServer(svc).Handler()
	tests := []struct {
		name           string
		taskID         domain.TaskID
		body           string
		wantStatus     int
		wantResultTask domain.TaskID
		wantEdges      int
		wantBinding    domain.MaterialID
		wantError      domain.ErrorCode
	}{
		{
			name: "first task executes", taskID: "task-A",
			body:       `{"parent":"pipe-1","child":"stub-1","kind":"STUB","length_mm":400,"bind_port":"drain-port"}`,
			wantStatus: http.StatusOK, wantResultTask: "task-A", wantEdges: 1, wantBinding: "stub-1",
		},
		{
			name: "same task canonical replay", taskID: "task-A",
			body:       `{ "bind_port":"drain-port", "length_mm":400, "kind":"STUB", "child":"stub-1", "parent":"pipe-1" }`,
			wantStatus: http.StatusOK, wantResultTask: "task-A", wantEdges: 1, wantBinding: "stub-1",
		},
		{
			name: "same task changed semantic body conflicts", taskID: "task-A",
			body:       `{"parent":"pipe-1","child":"stub-2","kind":"STUB","length_mm":300,"bind_port":"outlet-port"}`,
			wantStatus: http.StatusConflict, wantEdges: 1, wantBinding: "stub-1", wantError: domain.CodeIdempotencyConflict,
		},
		{
			name: "different task executes independently", taskID: "task-B",
			body:       `{"parent":"pipe-1","child":"stub-1","kind":"STUB","length_mm":400,"bind_port":"drain-port"}`,
			wantStatus: http.StatusOK, wantResultTask: "task-B", wantEdges: 1, wantBinding: "stub-1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+string(tc.taskID)+"/materials/operations", strings.NewReader(tc.body))
			req.Header.Set("Operation-Id", "shared-material-operation")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if tc.wantError != "" {
				var got httpapi.ErrorResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if got.Code != tc.wantError {
					t.Fatalf("error code = %q, want %q", got.Code, tc.wantError)
				}
			} else {
				var got app.MaterialOpResult
				if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
					t.Fatalf("decode material response: %v", err)
				}
				if got.TaskID != tc.wantResultTask {
					t.Fatalf("response task_id = %q, want %q", got.TaskID, tc.wantResultTask)
				}
			}

			state, ok := svc.GetTask(tc.taskID)
			if !ok {
				t.Fatalf("task %q disappeared", tc.taskID)
			}
			if len(state.Lineage.Edges) != tc.wantEdges {
				t.Fatalf("task %q lineage edges = %d, want %d", tc.taskID, len(state.Lineage.Edges), tc.wantEdges)
			}
			if got := state.PortBindings["drain-port"]; got != tc.wantBinding {
				t.Fatalf("task %q drain-port binding = %q, want %q", tc.taskID, got, tc.wantBinding)
			}
		})
	}
}
