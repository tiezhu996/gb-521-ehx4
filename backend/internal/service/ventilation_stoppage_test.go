package service

import (
	"testing"

	"mine-ventilation-network-simulator/backend/internal/constants"
	"mine-ventilation-network-simulator/backend/internal/model"
)

func stoppageTestGraphNodes() []model.VentilationNode {
	return []model.VentilationNode{
		{ID: 1, Code: "IN", NodeType: string(constants.NodeTypeIntake), Status: string(constants.NodeStatusActive)},
		{ID: 2, Code: "J", NodeType: string(constants.NodeTypeJunction), Status: string(constants.NodeStatusActive)},
		{ID: 3, Code: "WF", NodeType: string(constants.NodeTypeWorkface), RequiredAirflowM3S: 18, Status: string(constants.NodeStatusActive)},
		{ID: 4, Code: "OUT", NodeType: string(constants.NodeTypeExhaust), Status: string(constants.NodeStatusActive)},
	}
}

func stoppageTestEdges() []model.AirwayEdge {
	return []model.AirwayEdge{
		{ID: 1, Code: "E1", FromNodeID: 1, ToNodeID: 2, DoorState: string(constants.DoorStateOpen), Enabled: true},
		{ID: 2, Code: "E2", FromNodeID: 2, ToNodeID: 3, DoorState: string(constants.DoorStateOpen), Enabled: true},
		{ID: 3, Code: "E3", FromNodeID: 3, ToNodeID: 4, DoorState: string(constants.DoorStateOpen), Enabled: true},
		{ID: 4, Code: "E4", FromNodeID: 2, ToNodeID: 4, DoorState: string(constants.DoorStateRegulate), Enabled: true},
	}
}

func TestIdentifyCuttingEdgeNamesOnlyFeedAirway(t *testing.T) {
	graph := newAirflowGraph(stoppageTestGraphNodes(), stoppageTestEdges())
	id, code, _ := identifyCuttingEdge(3, graph, map[uint]bool{}, 2, map[uint]bool{2: true}, false)
	if id != 2 || code != "E2" {
		t.Fatalf("expected E2 to be the cutting edge, got id=%d code=%s", id, code)
	}
}

func TestIdentifyCuttingEdgeSurfacesPreExistingStoppage(t *testing.T) {
	graph := newAirflowGraph(stoppageTestGraphNodes(), stoppageTestEdges())
	active := map[uint]bool{2: true}
	id, code, reason := identifyCuttingEdge(3, graph, active, 1, map[uint]bool{1: true, 2: true}, true)
	if id != 2 || code != "E2" {
		t.Fatalf("expected pre-existing stoppage E2, got id=%d code=%s", id, code)
	}
	if reason == "" {
		t.Fatal("expected an explanatory reason for the pre-existing zero-air workface")
	}
}

func TestIdentifyCuttingEdgeFallsBackToShortestPathWhenTwoEdgesClosed(t *testing.T) {
	graph := newAirflowGraph(stoppageTestGraphNodes(), stoppageTestEdges())
	combined := map[uint]bool{1: true, 2: true}
	id, code, _ := identifyCuttingEdge(3, graph, map[uint]bool{}, 1, combined, false)
	if id != 1 || code != "E1" {
		t.Fatalf("expected upstream E1 via shortest path, got id=%d code=%s", id, code)
	}
}

func TestAirflowGraphVentilatedRequiresIntakeAndExhaustPath(t *testing.T) {
	graph := newAirflowGraph(stoppageTestGraphNodes(), stoppageTestEdges())
	if !graph.ventilated(3, map[uint]bool{}) {
		t.Fatal("workface should be ventilated in the open network")
	}
	if graph.ventilated(3, map[uint]bool{2: true}) {
		t.Fatal("workface cannot be ventilated when its only feed airway is closed")
	}
	if graph.ventilated(3, map[uint]bool{3: true}) {
		t.Fatal("workface cannot be ventilated when its only exhaust airway is closed")
	}
}

func TestAirflowGraphVentilatedKeepsBypassAirwayOpen(t *testing.T) {
	edges := append(stoppageTestEdges(),
		model.AirwayEdge{ID: 5, Code: "E5", FromNodeID: 1, ToNodeID: 3, DoorState: string(constants.DoorStateOpen), Enabled: true},
	)
	graph := newAirflowGraph(stoppageTestGraphNodes(), edges)
	if !graph.ventilated(3, map[uint]bool{2: true}) {
		t.Fatal("bypass airway E5 should keep the workface ventilated after E2 closes")
	}
}

func TestShortestPathEdgesReturnsDeterministicAirwaySequence(t *testing.T) {
	graph := newAirflowGraph(stoppageTestGraphNodes(), stoppageTestEdges())
	out := graph.adjacency(map[uint]bool{})
	path, ok := shortestPathEdges([]uint{1}, map[uint]bool{3: true}, out)
	if !ok || len(path) != 2 || path[0] != 1 || path[1] != 2 {
		t.Fatalf("unexpected shortest path E1,E2: %#v ok=%v", path, ok)
	}
	exhaustPath, ok := shortestPathEdges([]uint{3}, graph.exhausts, out)
	if !ok || len(exhaustPath) != 1 || exhaustPath[0] != 3 {
		t.Fatalf("unexpected exhaust path E3: %#v ok=%v", exhaustPath, ok)
	}
}

func TestWorkfaceAirflowsTakesLargerIncomingOutgoing(t *testing.T) {
	flows := map[uint]float64{1: 12, 2: 12, 3: 12, 4: 0.5}
	available := workfaceAirflows(stoppageTestEdges(), flows)
	if available[3] != 12 {
		t.Fatalf("expected workface airflow 12, got %.3f", available[3])
	}
}

func TestEdgesWithClosedDoorsDoesNotMutateInput(t *testing.T) {
	edges := stoppageTestEdges()
	projected := edgesWithClosedDoors(edges, map[uint]bool{2: true})
	if projected[1].DoorState != string(constants.DoorStateClosed) {
		t.Fatal("stopped airway should be treated as closed in projection")
	}
	if edges[1].DoorState != string(constants.DoorStateOpen) {
		t.Fatalf("baseline edge state must remain untouched, got %s", edges[1].DoorState)
	}
}
