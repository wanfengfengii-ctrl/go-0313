package app

import (
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func TestModel_MaterialOpUnknownPortRollsBackWorkingAndRecoveredState(t *testing.T) {
	svc, backing := newTestService(t)
	taskID := domain.TaskID("task-material-unknown-port-rollback")
	lockTask(t, svc, taskID, validGraph("zone-A", "drain-A", "seg-A", "out-A"),
		[]MaterialSpec{{ID: "pipe-1", Batch: "batch-1", Kind: domain.KindPipe, Length: 1000}})

	before, ok := svc.GetTask(taskID)
	if !ok {
		t.Fatal("task missing before material operation")
	}
	req := MaterialOpRequest{
		Parent:   "pipe-1",
		Child:    "ghost-stub",
		Kind:     domain.KindStub,
		Length:   100,
		BindPort: "missing-port",
	}
	_, err := svc.MaterialOp(taskID, "material-unknown-port", CanonicalDigest(req), req)
	stable, ok := err.(*domain.StableError)
	if !ok || stable.Code != domain.CodeNotFound {
		t.Fatalf("expected NOT_FOUND for unknown port, got %v", err)
	}

	restarted, err := NewService(backing, catalog.DemoSnapshot(), weld.PassThroughRegistry("welder-1"))
	if err != nil {
		t.Fatalf("restart service: %v", err)
	}
	recovered, ok := restarted.GetTask(taskID)
	if !ok {
		t.Fatal("task missing after restart")
	}
	immediate, ok := svc.GetTask(taskID)
	if !ok {
		t.Fatal("task missing after rejected material operation")
	}

	cases := []struct {
		name  string
		state *TaskState
	}{
		{name: "current service query", state: immediate},
		{name: "restart recovery", state: recovered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, exists := tc.state.Lineage.Nodes["ghost-stub"]; exists {
				t.Fatal("rejected child leaked into material nodes")
			}
			if _, exists := tc.state.Lineage.Dispositions["ghost-stub"]; exists {
				t.Fatal("rejected child leaked into material dispositions")
			}
			for _, edge := range tc.state.Lineage.Edges {
				if edge.Parent == "pipe-1" || edge.Child == "ghost-stub" {
					t.Fatalf("rejected conversion leaked lineage edge: %+v", edge)
				}
			}
			if got, want := tc.state.Lineage.Remaining("pipe-1"), before.Lineage.Remaining("pipe-1"); got != want {
				t.Fatalf("pipe-1 remaining length changed: got %d, want %d", got, want)
			}
			if _, exists := tc.state.PortBindings["missing-port"]; exists {
				t.Fatal("rejected child leaked a missing-port binding")
			}
			if got, want := len(tc.state.PortBindings), len(before.PortBindings); got != want {
				t.Fatalf("port binding count changed: got %d, want %d", got, want)
			}
			if got, want := len(tc.state.Events), len(before.Events); got != want {
				t.Fatalf("event appended for rejected operation: got %d events, want %d", got, want)
			}
			if got, want := tc.state.Task.EventSeq, before.Task.EventSeq; got != want {
				t.Fatalf("event sequence advanced for rejected operation: got %d, want %d", got, want)
			}
		})
	}
}
