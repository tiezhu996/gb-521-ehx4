package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"mine-ventilation-network-simulator/backend/internal/constants"
	"mine-ventilation-network-simulator/backend/internal/dto"
	"mine-ventilation-network-simulator/backend/internal/model"
	"mine-ventilation-network-simulator/backend/internal/repository"
	"mine-ventilation-network-simulator/backend/pkg/api"
)

func newStoppageTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:stoppage-test-%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.VentilationNode{}, &model.AirwayEdge{},
		&model.FanScenario{}, &model.SimulationRun{}, &model.VentilationStoppage{}, &model.AuditEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// seedStoppageNetwork 建一张带冗余馈风路径的风网：WF 由 E1→E2 与 E5→E6 两条
// 馈风路径共同供风（基线约 34 m³/s，需风量 30），关掉其中一条后降到需风量
// 以下但仍远大于无风阈值；WF2 由 E7、E9 两条小阻抗支路供风，需风量仅 1。
// E3 是 WF 唯一回风通道，关闭即无风。
func seedStoppageNetwork(t *testing.T, db *gorm.DB) {
	t.Helper()
	nodes := []model.VentilationNode{
		{Code: "IN", NodeType: string(constants.NodeTypeIntake), PressurePa: 1250, Status: string(constants.NodeStatusActive)},
		{Code: "J", NodeType: string(constants.NodeTypeJunction), PressurePa: 680, Status: string(constants.NodeStatusActive)},
		{Code: "WF", NodeType: string(constants.NodeTypeWorkface), RequiredAirflowM3S: 30, PressurePa: 410, Status: string(constants.NodeStatusActive)},
		{Code: "OUT", NodeType: string(constants.NodeTypeExhaust), PressurePa: 0, Status: string(constants.NodeStatusActive)},
		{Code: "J2", NodeType: string(constants.NodeTypeJunction), PressurePa: 600, Status: string(constants.NodeStatusActive)},
		{Code: "WF2", NodeType: string(constants.NodeTypeWorkface), RequiredAirflowM3S: 1, PressurePa: 300, Status: string(constants.NodeStatusActive)},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatalf("seed nodes: %v", err)
	}
	byCode := map[string]uint{}
	for _, node := range nodes {
		byCode[node.Code] = node.ID
	}
	edges := []model.AirwayEdge{
		{Code: "E1", FromNodeID: byCode["IN"], ToNodeID: byCode["J"], ResistanceNS2M8: 1.0, AreaM2: 8.2, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, Version: 1},
		{Code: "E2", FromNodeID: byCode["J"], ToNodeID: byCode["WF"], ResistanceNS2M8: 1.5, AreaM2: 6.4, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, Version: 1},
		{Code: "E3", FromNodeID: byCode["WF"], ToNodeID: byCode["OUT"], ResistanceNS2M8: 1.0, AreaM2: 7.0, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, Version: 1},
		{Code: "E5", FromNodeID: byCode["IN"], ToNodeID: byCode["J2"], ResistanceNS2M8: 1.0, AreaM2: 8.2, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, Version: 1},
		{Code: "E6", FromNodeID: byCode["J2"], ToNodeID: byCode["WF"], ResistanceNS2M8: 1.5, AreaM2: 6.4, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, Version: 1},
		{Code: "E7", FromNodeID: byCode["J"], ToNodeID: byCode["WF2"], ResistanceNS2M8: 4.0, AreaM2: 5.0, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, Version: 1},
		{Code: "E8", FromNodeID: byCode["WF2"], ToNodeID: byCode["OUT"], ResistanceNS2M8: 4.0, AreaM2: 5.0, MaxVelocityMS: 8, DoorState: string(constants.DoorStateOpen), Enabled: true, Version: 1},
		{Code: "E9", FromNodeID: byCode["J2"], ToNodeID: byCode["WF2"], ResistanceNS2M8: 4.0, AreaM2: 5.0, MaxVelocityMS: 8, DoorState: string(constants.DoorStateRegulate), Enabled: true, Version: 1},
	}
	if err := db.Create(&edges).Error; err != nil {
		t.Fatalf("seed edges: %v", err)
	}
	scenario := model.FanScenario{
		Name: "基准方案", Description: "停风测试基准", OperatingMode: "normal",
		FanCurveJSON:   []byte(`[{"flow_m3s":0,"pressure_pa":1450},{"flow_m3s":30,"pressure_pa":1180},{"flow_m3s":60,"pressure_pa":720}]`),
		ScenarioStatus: string(constants.ScenarioStatusApproved), SolverTolerance: 0.02, MaxIterations: 120,
		Version: 1, CreatedBy: 1,
	}
	if err := db.Create(&scenario).Error; err != nil {
		t.Fatalf("seed scenario: %v", err)
	}
}

func newStoppageService(db *gorm.DB) *VentilationStoppageService {
	return NewVentilationStoppageService(
		repository.NewVentilationStoppageRepository(db),
		repository.NewAirwayEdgeRepository(db),
		repository.NewVentilationNodeRepository(db),
		repository.NewFanScenarioRepository(db),
	)
}

func stoppageActor() Actor {
	return Actor{ID: 7, Email: "engineer@mine.local", Name: "测试工程师", Role: string(constants.RoleEngineer), RequestID: "test-request"}
}

func edgeIDByCode(t *testing.T, db *gorm.DB, code string) uint {
	t.Helper()
	var edge model.AirwayEdge
	if err := db.Where("code = ?", code).First(&edge).Error; err != nil {
		t.Fatalf("load edge %s: %v", code, err)
	}
	return edge.ID
}

func nodeIDByCode(t *testing.T, db *gorm.DB, code string) uint {
	t.Helper()
	var node model.VentilationNode
	if err := db.Where("code = ?", code).First(&node).Error; err != nil {
		t.Fatalf("load node %s: %v", code, err)
	}
	return node.ID
}

func appErrorCode(err error) string {
	var appErr *api.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return ""
}

func futureRestore(hours int) time.Time {
	return time.Now().UTC().Add(time.Duration(hours) * time.Hour)
}

func TestStoppageCreateRejectsZeroAirflowWorkface(t *testing.T) {
	db := newStoppageTestDB(t)
	seedStoppageNetwork(t, db)
	svc := newStoppageService(db)
	_, err := svc.Create(context.Background(), dto.CreateStoppageRequest{
		EdgeID: edgeIDByCode(t, db, "E3"), Reason: "检修回风风门",
		PlannedRestoreAt: futureRestore(2),
	}, stoppageActor())
	if appErrorCode(err) != "WORKFACE_ZERO_AIRFLOW" {
		t.Fatalf("expected WORKFACE_ZERO_AIRFLOW, got %v", err)
	}
	var appErr *api.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T", err)
	}
	zeros, ok := appErr.Details.([]dto.StoppageZeroAirWorkface)
	if !ok || len(zeros) != 1 || zeros[0].WorkfaceCode != "WF" || zeros[0].CuttingCode != "E3" {
		t.Fatalf("expected rejection naming E3 as cutting edge, got %#v", appErr.Details)
	}
	var count int64
	if err := db.Model(&model.VentilationStoppage{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected registration must not persist, count=%d err=%v", count, err)
	}
}

func TestStoppagePreviewListsShortfallWorkfaceAndEdgeLosses(t *testing.T) {
	db := newStoppageTestDB(t)
	seedStoppageNetwork(t, db)
	svc := newStoppageService(db)
	assessment, err := svc.Preview(context.Background(), dto.PreviewStoppageRequest{
		EdgeID: edgeIDByCode(t, db, "E6"), PlannedRestoreAt: futureRestore(3),
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !assessment.CanRegister || len(assessment.ZeroAirWorkfaces) != 0 {
		t.Fatalf("feeder stoppage should stay registrable: %#v", assessment.ZeroAirWorkfaces)
	}
	if len(assessment.WorkfaceImpacts) != 1 || assessment.WorkfaceImpacts[0].WorkfaceCode != "WF" {
		t.Fatalf("expected WF shortfall, got %#v", assessment.WorkfaceImpacts)
	}
	impact := assessment.WorkfaceImpacts[0]
	if !impact.ArrangementNeeded || impact.ProjectedAirflow >= impact.RequiredAirflow || impact.ProjectedAirflow < stoppageZeroAirThreshold {
		t.Fatalf("expected ventilated shortfall needing arrangement, got %#v", impact)
	}
	if impact.Shortfall <= 0 || impact.BaselineAirflow < impact.RequiredAirflow {
		t.Fatalf("baseline should satisfy demand and shortfall must be positive: %#v", impact)
	}
	losses := map[string]dto.StoppageEdgeFlowLoss{}
	for _, loss := range assessment.EdgeFlowLosses {
		losses[loss.EdgeCode] = loss
	}
	e6, ok := losses["E6"]
	if !ok || !e6.Closed || e6.AlreadyStopped || e6.ProjectedFlow != 0 || e6.FlowLoss <= 0 {
		t.Fatalf("E6 should appear as newly closed with positive loss: %#v", assessment.EdgeFlowLosses)
	}
}

func TestStoppageCreateRequiresArrangementAndCombinesActiveStoppages(t *testing.T) {
	db := newStoppageTestDB(t)
	seedStoppageNetwork(t, db)
	svc := newStoppageService(db)
	actor := stoppageActor()
	ctx := context.Background()
	wfID := nodeIDByCode(t, db, "WF")
	e6ID := edgeIDByCode(t, db, "E6")

	missing, err := svc.Create(ctx, dto.CreateStoppageRequest{
		EdgeID: e6ID, Reason: "检修风门更换", PlannedRestoreAt: futureRestore(3),
	}, actor)
	if appErrorCode(err) != "ARRANGEMENT_REQUIRED" || missing != nil {
		t.Fatalf("expected ARRANGEMENT_REQUIRED, got record=%#v err=%v", missing, err)
	}

	first, err := svc.Create(ctx, dto.CreateStoppageRequest{
		EdgeID: e6ID, Reason: "检修风门更换", PlannedRestoreAt: futureRestore(3),
		Arrangements: []dto.WorkfaceArrangement{
			{WorkfaceNodeID: wfID, Arrangement: "WF 减产运行，瓦斯员现场盯守，每小时测风一次"},
		},
	}, actor)
	if err != nil {
		t.Fatalf("register with arrangement: %v", err)
	}
	if first.Status != string(constants.StoppageStatusActive) || first.BaselineDoorState != string(constants.DoorStateOpen) {
		t.Fatalf("unexpected stoppage state: %#v", first)
	}
	var impact dto.StoppageAssessment
	if err := json.Unmarshal(first.ImpactJSON, &impact); err != nil || len(impact.WorkfaceImpacts) != 1 {
		t.Fatalf("stored impact snapshot invalid: %v %#v", err, impact)
	}

	if _, err := svc.Create(ctx, dto.CreateStoppageRequest{
		EdgeID: e6ID, Reason: "重复登记测试", PlannedRestoreAt: futureRestore(4),
	}, actor); appErrorCode(err) != "STOPPAGE_ALREADY_ACTIVE" {
		t.Fatalf("expected STOPPAGE_ALREADY_ACTIVE, got %v", err)
	}

	// E6 已在停风时再关另一条馈风巷道 E2：两条馈风路径都断，WF 无风，必须
	// 驳回并指出是 E2（本次新增）切断了风路。
	zeroSecond, err := svc.Create(ctx, dto.CreateStoppageRequest{
		EdgeID: edgeIDByCode(t, db, "E2"), Reason: "叠加停风验证", PlannedRestoreAt: futureRestore(5),
		Arrangements: []dto.WorkfaceArrangement{
			{WorkfaceNodeID: wfID, Arrangement: "WF 停产撤人安排"},
		},
	}, actor)
	if appErrorCode(err) != "WORKFACE_ZERO_AIRFLOW" || zeroSecond != nil {
		t.Fatalf("expected WORKFACE_ZERO_AIRFLOW for combined feeders, got record=%#v err=%v", zeroSecond, err)
	}
	var zeroErr *api.AppError
	errors.As(err, &zeroErr)
	zeros, _ := zeroErr.Details.([]dto.StoppageZeroAirWorkface)
	foundCut := false
	for _, zero := range zeros {
		if zero.WorkfaceCode == "WF" && zero.CuttingCode == "E2" {
			foundCut = true
		}
	}
	if !foundCut {
		t.Fatalf("expected E2 named as the cutting edge, got %#v", zeros)
	}

	combined, err := svc.Preview(ctx, dto.PreviewStoppageRequest{
		EdgeID: edgeIDByCode(t, db, "E7"), PlannedRestoreAt: futureRestore(5),
	})
	if err != nil {
		t.Fatalf("combined preview: %v", err)
	}
	if len(combined.ActiveEdgeIDs) != 1 || combined.ActiveEdgeIDs[0] != e6ID || len(combined.CombinedEdgeIDs) != 2 {
		t.Fatalf("active stoppage must be combined into projection: %#v", combined)
	}
	flagged := false
	for _, loss := range combined.EdgeFlowLosses {
		if loss.EdgeID == e6ID && loss.AlreadyStopped && loss.Closed {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("E6 must be flagged as already stopped in combined losses: %#v", combined.EdgeFlowLosses)
	}

	second, err := svc.Create(ctx, dto.CreateStoppageRequest{
		EdgeID: edgeIDByCode(t, db, "E7"), Reason: "分支检修停风", PlannedRestoreAt: futureRestore(5),
		Arrangements: []dto.WorkfaceArrangement{
			{WorkfaceNodeID: wfID, Arrangement: "WF 维持减产，停风期间每半小时测风"},
		},
	}, actor)
	if err != nil {
		t.Fatalf("combined registration: %v", err)
	}
	var stored dto.StoppageAssessment
	if err := json.Unmarshal(second.ImpactJSON, &stored); err != nil {
		t.Fatalf("decode combined impact: %v", err)
	}
	if len(stored.CombinedEdgeIDs) != 2 || len(stored.ActiveEdgeIDs) != 1 {
		t.Fatalf("stored impact must combine active stoppages: %#v", stored)
	}
}

func TestStoppageRecoverRestoresBaselineAndAudits(t *testing.T) {
	db := newStoppageTestDB(t)
	seedStoppageNetwork(t, db)
	svc := newStoppageService(db)
	actor := stoppageActor()
	ctx := context.Background()
	wfID := nodeIDByCode(t, db, "WF")
	e6ID := edgeIDByCode(t, db, "E6")
	arrangements := []dto.WorkfaceArrangement{
		{WorkfaceNodeID: wfID, Arrangement: "WF 减产运行，瓦斯员现场盯守"},
	}

	created, err := svc.Create(ctx, dto.CreateStoppageRequest{
		EdgeID: e6ID, Reason: "检修风门更换", PlannedRestoreAt: futureRestore(3), Arrangements: arrangements,
	}, actor)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	recovered, err := svc.Recover(ctx, created.ID, "现场测风正常，风门已恢复", actor)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered.Status != string(constants.StoppageStatusRecovered) || recovered.ActualRestoreAt == nil || recovered.RecoveredBy == nil {
		t.Fatalf("unexpected recovered state: %#v", recovered)
	}
	if _, err := svc.Recover(ctx, created.ID, "", actor); appErrorCode(err) != "STOPPAGE_ALREADY_RECOVERED" {
		t.Fatalf("expected STOPPAGE_ALREADY_RECOVERED, got %v", err)
	}
	if _, err := svc.stoppages.FindActiveByEdge(ctx, e6ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("edge should have no active stoppage after recovery, got %v", err)
	}
	// 恢复后同一条巷道可以再次登记，推演回到登记前状态。
	again, err := svc.Create(ctx, dto.CreateStoppageRequest{
		EdgeID: e6ID, Reason: "恢复后再次停风", PlannedRestoreAt: futureRestore(6), Arrangements: arrangements,
	}, actor)
	if err != nil {
		t.Fatalf("register after recovery: %v", err)
	}
	if again.Status != string(constants.StoppageStatusActive) {
		t.Fatalf("expected active stoppage after re-registration, got %#v", again)
	}
	var actions []string
	if err := db.Model(&model.AuditEvent{}).Where("entity_type = ?", "ventilation_stoppage").Order("id ASC").Pluck("action", &actions).Error; err != nil {
		t.Fatalf("load audits: %v", err)
	}
	want := []string{"ventilation_stoppage.registered", "ventilation_stoppage.recovered", "ventilation_stoppage.registered"}
	if len(actions) != len(want) {
		t.Fatalf("registration and recovery must both be audited, got %#v", actions)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("audit action %d: want %s got %s (%#v)", i, want[i], actions[i], actions)
		}
	}
}
