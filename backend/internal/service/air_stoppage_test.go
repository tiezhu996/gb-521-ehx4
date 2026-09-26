package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"gorm.io/datatypes"

	"mine-ventilation-network-simulator/backend/internal/constants"
	"mine-ventilation-network-simulator/backend/internal/dto"
	"mine-ventilation-network-simulator/backend/internal/model"
)

func stoppageTestScenario() model.FanScenario {
	curve, _ := json.Marshal([]dto.FanCurvePoint{{FlowM3S: 0, PressurePa: 900}, {FlowM3S: 30, PressurePa: 600}, {FlowM3S: 60, PressurePa: 300}})
	return model.FanScenario{ID: 7, FanCurveJSON: datatypes.JSON(curve), SolverTolerance: 0.02, MaxIterations: 120}
}

func stoppageTestNodes(required float64) []model.VentilationNode {
	return []model.VentilationNode{
		{ID: 1, Code: "IN", NodeType: string(constants.NodeTypeIntake), Status: string(constants.NodeStatusActive), PressurePa: 1000},
		{ID: 2, Code: "JCT", NodeType: string(constants.NodeTypeJunction), Status: string(constants.NodeStatusActive)},
		{ID: 3, Code: "WF", NodeType: string(constants.NodeTypeWorkface), Status: string(constants.NodeStatusActive), RequiredAirflowM3S: required},
		{ID: 4, Code: "OUT", NodeType: string(constants.NodeTypeExhaust), Status: string(constants.NodeStatusActive), PressurePa: 0},
	}
}

func stoppageEdge(id, from, to uint, resistance float64) model.AirwayEdge {
	return model.AirwayEdge{
		ID: id, Code: fmt.Sprintf("E%d", id), FromNodeID: from, ToNodeID: to,
		ResistanceNS2M8: resistance, AreaM2: 6, MaxVelocityMS: 8,
		DoorState: string(constants.DoorStateOpen), Enabled: true,
	}
}

func TestEvaluateStoppageImpactRejectsZeroAirAndNamesCutEdge(t *testing.T) {
	nodes := stoppageTestNodes(8)
	edges := []model.AirwayEdge{stoppageEdge(1, 1, 3, 2), stoppageEdge(2, 3, 4, 2)}
	evaluation := evaluateStoppageImpact(stoppageTestScenario(), nodes, edges, []uint{1})
	if len(evaluation.ZeroAirWorkfaces) != 1 {
		t.Fatalf("expected one zero-air workface, got %#v", evaluation.ZeroAirWorkfaces)
	}
	zero := evaluation.ZeroAirWorkfaces[0]
	if !reflect.DeepEqual(zero.CutEdgeIDs, []uint{1}) {
		t.Fatalf("expected cut edge [1], got %v", zero.CutEdgeIDs)
	}
	if len(zero.CutEdgeCodes) != 1 || zero.CutEdgeCodes[0] != "E1" {
		t.Fatalf("expected cut edge code E1, got %v", zero.CutEdgeCodes)
	}
	if len(evaluation.Deficits) != 0 {
		t.Fatalf("zero-air workface must not appear as deficit: %#v", evaluation.Deficits)
	}
}

func TestEvaluateStoppageImpactListsDeficitAndEdgeLoss(t *testing.T) {
	nodes := stoppageTestNodes(18)
	edges := []model.AirwayEdge{
		stoppageEdge(1, 1, 2, 1),
		stoppageEdge(2, 2, 3, 1),
		stoppageEdge(3, 3, 4, 1),
		stoppageEdge(5, 2, 3, 20),
	}
	evaluation := evaluateStoppageImpact(stoppageTestScenario(), nodes, edges, []uint{2})
	if len(evaluation.ZeroAirWorkfaces) != 0 {
		t.Fatalf("parallel path must keep air, got zero-air %#v", evaluation.ZeroAirWorkfaces)
	}
	if len(evaluation.Deficits) != 1 || evaluation.Deficits[0].NodeID != 3 {
		t.Fatalf("expected deficit on workface 3, got %#v", evaluation.Deficits)
	}
	deficit := evaluation.Deficits[0]
	if deficit.StoppedM3S <= zeroAirEpsilon || deficit.StoppedM3S >= deficit.RequiredM3S {
		t.Fatalf("stopped flow must stay below required but above zero: %#v", deficit)
	}
	if deficit.NormalM3S < deficit.RequiredM3S {
		t.Fatalf("normal flow should cover the requirement in this fixture: %#v", deficit)
	}
	losses := map[uint]float64{}
	for _, loss := range evaluation.EdgeLosses {
		losses[loss.EdgeID] = loss.LossM3S
	}
	if losses[2] <= 0 {
		t.Fatalf("stopped edge 2 must report its lost flow, got %#v", evaluation.EdgeLosses)
	}
	if _, ok := losses[5]; ok {
		t.Fatalf("bypass edge 5 gains flow and must not be listed as loss: %#v", evaluation.EdgeLosses)
	}
	if !reflect.DeepEqual(evaluation.IncludedEdgeIDs, []uint{2}) {
		t.Fatalf("unexpected included edges %v", evaluation.IncludedEdgeIDs)
	}
}

func TestEvaluateStoppageImpactCombinesExistingStoppages(t *testing.T) {
	nodes := stoppageTestNodes(8)
	edges := []model.AirwayEdge{
		stoppageEdge(1, 1, 2, 1),
		stoppageEdge(2, 2, 3, 2),
		stoppageEdge(3, 3, 4, 1),
		stoppageEdge(5, 2, 3, 2),
	}
	single := evaluateStoppageImpact(stoppageTestScenario(), nodes, edges, []uint{2})
	if len(single.ZeroAirWorkfaces) != 0 {
		t.Fatalf("single stoppage must keep the parallel path alive: %#v", single.ZeroAirWorkfaces)
	}
	combined := evaluateStoppageImpact(stoppageTestScenario(), nodes, edges, []uint{2, 5})
	if len(combined.ZeroAirWorkfaces) != 1 {
		t.Fatalf("combined stoppages must cut the workface: %#v", combined.ZeroAirWorkfaces)
	}
	cuts := combined.ZeroAirWorkfaces[0].CutEdgeIDs
	if !reflect.DeepEqual(cuts, []uint{2, 5}) {
		t.Fatalf("expected both parallel edges named as cuts, got %v", cuts)
	}
}

func TestEvaluateStoppageImpactIsDeterministic(t *testing.T) {
	nodes := stoppageTestNodes(18)
	edges := []model.AirwayEdge{
		stoppageEdge(1, 1, 2, 1),
		stoppageEdge(2, 2, 3, 1),
		stoppageEdge(3, 3, 4, 1),
		stoppageEdge(5, 2, 3, 20),
	}
	first := evaluateStoppageImpact(stoppageTestScenario(), nodes, edges, []uint{5, 2})
	second := evaluateStoppageImpact(stoppageTestScenario(), nodes, edges, []uint{2, 5})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("evaluation must be deterministic regardless of input order")
	}
}

func TestApplyStoppageOverlayForcesClosedDoorsWithoutMutatingInput(t *testing.T) {
	edges := []model.AirwayEdge{stoppageEdge(1, 1, 2, 1), stoppageEdge(2, 2, 3, 1)}
	overlaid := applyStoppageOverlay(edges, []uint{2})
	if overlaid[1].DoorState != string(constants.DoorStateClosed) {
		t.Fatalf("edge 2 must be closed in overlay, got %s", overlaid[1].DoorState)
	}
	if overlaid[0].DoorState != string(constants.DoorStateOpen) {
		t.Fatalf("edge 1 must stay open, got %s", overlaid[0].DoorState)
	}
	if edges[1].DoorState != string(constants.DoorStateOpen) {
		t.Fatal("overlay must not mutate the original edge state")
	}
	untouched := applyStoppageOverlay(edges, nil)
	if !reflect.DeepEqual(untouched, edges) {
		t.Fatal("empty stoppage list must return the original edges")
	}
}
