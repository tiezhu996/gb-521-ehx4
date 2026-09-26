package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"mine-ventilation-network-simulator/backend/internal/constants"
	"mine-ventilation-network-simulator/backend/internal/dto"
	"mine-ventilation-network-simulator/backend/internal/model"
	"mine-ventilation-network-simulator/backend/internal/repository"
	"mine-ventilation-network-simulator/backend/pkg/api"
)

func newStoppageServiceDB(t *testing.T) (*gorm.DB, *AirStoppageService, *SimulationService, []model.VentilationNode, []model.AirwayEdge) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.VentilationNode{}, &model.AirwayEdge{},
		&model.FanScenario{}, &model.SimulationRun{}, &model.AirStoppage{}, &model.AuditEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	nodes := []model.VentilationNode{
		{Code: "IN", NodeType: string(constants.NodeTypeIntake), Status: string(constants.NodeStatusActive), PressurePa: 1000},
		{Code: "JCT", NodeType: string(constants.NodeTypeJunction), Status: string(constants.NodeStatusActive)},
		{Code: "J2", NodeType: string(constants.NodeTypeJunction), Status: string(constants.NodeStatusActive)},
		{Code: "WF", NodeType: string(constants.NodeTypeWorkface), Status: string(constants.NodeStatusActive), RequiredAirflowM3S: 18},
		{Code: "OUT", NodeType: string(constants.NodeTypeExhaust), Status: string(constants.NodeStatusActive)},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatalf("create nodes: %v", err)
	}
	edges := []model.AirwayEdge{
		{Code: "E1", FromNodeID: nodes[0].ID, ToNodeID: nodes[1].ID, ResistanceNS2M8: 1, AreaM2: 6, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, CriticalPath: true},
		{Code: "E2", FromNodeID: nodes[1].ID, ToNodeID: nodes[3].ID, ResistanceNS2M8: 1, AreaM2: 6, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, CriticalPath: true},
		{Code: "E3", FromNodeID: nodes[3].ID, ToNodeID: nodes[4].ID, ResistanceNS2M8: 1, AreaM2: 6, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, CriticalPath: true},
		{Code: "E5", FromNodeID: nodes[1].ID, ToNodeID: nodes[2].ID, ResistanceNS2M8: 1, AreaM2: 6, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true},
		{Code: "E6", FromNodeID: nodes[2].ID, ToNodeID: nodes[3].ID, ResistanceNS2M8: 19, AreaM2: 6, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true},
	}
	if err := db.Create(&edges).Error; err != nil {
		t.Fatalf("create edges: %v", err)
	}
	curve := datatypes.JSON([]byte(`[{"flow_m3s":0,"pressure_pa":900},{"flow_m3s":30,"pressure_pa":600},{"flow_m3s":60,"pressure_pa":300}]`))
	scenario := model.FanScenario{
		Name: "已批准方案", Description: "停风评估基准", FanCurveJSON: curve, OperatingMode: "normal",
		ScenarioStatus: string(constants.ScenarioStatusApproved), SolverTolerance: 0.02, MaxIterations: 120, CreatedBy: 1,
	}
	if err := db.Create(&scenario).Error; err != nil {
		t.Fatalf("create scenario: %v", err)
	}
	edgeRepo := repository.NewAirwayEdgeRepository(db)
	nodeRepo := repository.NewVentilationNodeRepository(db)
	scenarioRepo := repository.NewFanScenarioRepository(db)
	stoppageRepo := repository.NewAirStoppageRepository(db)
	runRepo := repository.NewSimulationRunRepository(db)
	stoppageService := NewAirStoppageService(stoppageRepo, edgeRepo, nodeRepo, scenarioRepo)
	simulationService := NewSimulationService(runRepo, scenarioRepo, nodeRepo, edgeRepo, stoppageRepo)
	return db, stoppageService, simulationService, nodes, edges
}

func stoppageCreateRequest(edgeID uint, workfaceID uint, withArrangement bool) dto.CreateAirStoppageRequest {
	req := dto.CreateAirStoppageRequest{
		EdgeID: edgeID, Reason: "检修皮带巷风门，按停风规程登记",
		PlannedRestoreAt: time.Now().Add(2 * time.Hour).Format(time.RFC3339),
	}
	if withArrangement {
		req.Arrangements = []dto.StoppageArrangementInput{{NodeID: workfaceID, Note: "撤出人员、切断该工作面电源、悬挂警示牌，安排专人测风"}}
	}
	return req
}

func stoppageActor() Actor {
	return Actor{ID: 9, Email: "engineer@mine.local", Name: "通风工程师", Role: string(constants.RoleEngineer), RequestID: "req-stoppage-test"}
}

func TestAirStoppageRegisterFlow(t *testing.T) {
	db, stoppageService, _, nodes, edges := newStoppageServiceDB(t)
	ctx := context.Background()
	workfaceID := nodes[3].ID
	mainEdgeID := edges[1].ID
	bypassEdgeID := edges[3].ID
	actor := stoppageActor()

	preview, err := stoppageService.Preview(ctx, mainEdgeID)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}
	if len(preview.Deficits) != 1 || preview.Deficits[0].NodeID != workfaceID {
		t.Fatalf("expected one deficit on workface, got %#v", preview.Deficits)
	}
	if len(preview.IncludedEdgeIDs) != 1 || preview.IncludedEdgeIDs[0] != mainEdgeID {
		t.Fatalf("expected only the new edge included, got %v", preview.IncludedEdgeIDs)
	}

	if _, err := stoppageService.Create(ctx, stoppageCreateRequest(mainEdgeID, workfaceID, false), actor); err == nil {
		t.Fatal("missing arrangement must be rejected")
	} else if appErr, ok := err.(*api.AppError); !ok || appErr.Code != "STOPPAGE_ARRANGEMENT_REQUIRED" {
		t.Fatalf("expected STOPPAGE_ARRANGEMENT_REQUIRED, got %v", err)
	}

	created, err := stoppageService.Create(ctx, stoppageCreateRequest(mainEdgeID, workfaceID, true), actor)
	if err != nil {
		t.Fatalf("create with arrangement failed: %v", err)
	}
	if created.Status != string(constants.StoppageStatusStopping) || created.EdgeID != mainEdgeID {
		t.Fatalf("unexpected created stoppage: %#v", created)
	}
	var storedEvaluation dto.StoppageEvaluation
	if err := json.Unmarshal(created.EvaluationJSON, &storedEvaluation); err != nil {
		t.Fatalf("evaluation json invalid: %v", err)
	}
	if len(storedEvaluation.Deficits) != 1 || storedEvaluation.Deficits[0].Arrangement == "" {
		t.Fatalf("stored evaluation must retain arrangement: %#v", storedEvaluation.Deficits)
	}

	if _, err := stoppageService.Create(ctx, stoppageCreateRequest(mainEdgeID, workfaceID, true), actor); err == nil {
		t.Fatal("second active stoppage on same edge must be rejected")
	} else if appErr, ok := err.(*api.AppError); !ok || appErr.Code != "STOPPAGE_ALREADY_ACTIVE" {
		t.Fatalf("expected STOPPAGE_ALREADY_ACTIVE, got %v", err)
	}

	if _, err := stoppageService.Create(ctx, stoppageCreateRequest(bypassEdgeID, workfaceID, true), actor); err == nil {
		t.Fatal("closing both parallel paths must be rejected")
	} else if appErr, ok := err.(*api.AppError); !ok || appErr.Code != "STOPPAGE_ZERO_AIR" {
		t.Fatalf("expected STOPPAGE_ZERO_AIR, got %v", err)
	}

	restored, err := stoppageService.Restore(ctx, created.ID, actor)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if restored.Status != string(constants.StoppageStatusRestored) || restored.RestoredBy == nil || restored.RestoredAt == nil {
		t.Fatalf("restored stoppage incomplete: %#v", restored)
	}
	if _, err := stoppageService.Restore(ctx, created.ID, actor); err == nil {
		t.Fatal("restoring twice must fail")
	} else if appErr, ok := err.(*api.AppError); !ok || appErr.Code != "STOPPAGE_NOT_ACTIVE" {
		t.Fatalf("expected STOPPAGE_NOT_ACTIVE, got %v", err)
	}

	afterRestore, err := stoppageService.Create(ctx, stoppageCreateRequest(mainEdgeID, workfaceID, true), actor)
	if err != nil {
		t.Fatalf("re-register after restore must succeed: %v", err)
	}
	if afterRestore.ID == created.ID {
		t.Fatal("re-registration must create a new record, not reuse the old one")
	}

	var events []model.AuditEvent
	if err := db.Where("entity_type = ?", "air_stoppage").Order("id ASC").Find(&events).Error; err != nil {
		t.Fatalf("list audits: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected create + restore + re-create audits, got %d", len(events))
	}
	if events[0].Action != "air_stoppage.created" || events[1].Action != "air_stoppage.restored" || events[2].Action != "air_stoppage.created" {
		t.Fatalf("unexpected audit actions: %v", []string{events[0].Action, events[1].Action, events[2].Action})
	}
}

func TestSimulationStartAppliesActiveStoppageOverlay(t *testing.T) {
	_, stoppageService, simulationService, nodes, edges := newStoppageServiceDB(t)
	ctx := context.Background()
	actor := stoppageActor()
	scenario, err := simulationService.scenarios.LatestApproved(ctx)
	if err != nil {
		t.Fatalf("find scenario: %v", err)
	}
	workfaceFlow := func(run *model.SimulationRun) float64 {
		var flows map[uint]float64
		if err := json.Unmarshal(run.EdgeFlowsJSON, &flows); err != nil {
			t.Fatalf("edge flows invalid: %v", err)
		}
		total := 0.0
		for _, edge := range edges {
			if edge.ToNodeID == nodes[3].ID {
				total += absFloat(flows[edge.ID])
			}
		}
		return total
	}
	normalRun, err := simulationService.Start(ctx, scenario.ID, actor)
	if err != nil {
		t.Fatalf("normal simulation failed: %v", err)
	}
	before := workfaceFlow(normalRun)

	if _, err := stoppageService.Create(ctx, stoppageCreateRequest(edges[1].ID, nodes[3].ID, true), actor); err != nil {
		t.Fatalf("create stoppage: %v", err)
	}
	stoppedRun, err := simulationService.Start(ctx, scenario.ID, actor)
	if err != nil {
		t.Fatalf("stopped simulation failed: %v", err)
	}
	after := workfaceFlow(stoppedRun)
	if !(after < before) {
		t.Fatalf("stoppage overlay must reduce workface flow: before=%.4f after=%.4f", before, after)
	}
	var snapshot struct {
		StoppageEdgeIDs []uint `json:"stoppage_edge_ids"`
		Edges           []model.AirwayEdge
	}
	if err := json.Unmarshal(stoppedRun.InputSnapshotJSON, &snapshot); err != nil {
		t.Fatalf("snapshot invalid: %v", err)
	}
	if len(snapshot.StoppageEdgeIDs) != 1 || snapshot.StoppageEdgeIDs[0] != edges[1].ID {
		t.Fatalf("snapshot must record stoppage edge ids: %v", snapshot.StoppageEdgeIDs)
	}
	for _, edge := range snapshot.Edges {
		if edge.ID == edges[1].ID && edge.DoorState != string(constants.DoorStateClosed) {
			t.Fatalf("snapshot edge must be closed during stoppage, got %s", edge.DoorState)
		}
	}

	persisted, err := simulationService.edges.Find(ctx, edges[1].ID)
	if err != nil {
		t.Fatalf("reload edge: %v", err)
	}
	if persisted.DoorState != string(constants.DoorStateOpen) {
		t.Fatalf("real door state must stay open in database, got %s", persisted.DoorState)
	}
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
