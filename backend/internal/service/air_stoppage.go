package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"mine-ventilation-network-simulator/backend/internal/constants"
	"mine-ventilation-network-simulator/backend/internal/dto"
	"mine-ventilation-network-simulator/backend/internal/model"
	"mine-ventilation-network-simulator/backend/internal/repository"
	"mine-ventilation-network-simulator/backend/pkg/api"
)

const (
	zeroAirEpsilon   = 0.01
	airLossEpsilon   = 0.01
	minNoteRunes     = 4
	stoppageEntity   = "停风登记"
	airwayEdgeEntity = "巷道边"
)

type AirStoppageService struct {
	stoppages *repository.AirStoppageRepository
	edges     *repository.AirwayEdgeRepository
	nodes     *repository.VentilationNodeRepository
	scenarios *repository.FanScenarioRepository
}

func NewAirStoppageService(stoppages *repository.AirStoppageRepository, edges *repository.AirwayEdgeRepository, nodes *repository.VentilationNodeRepository, scenarios *repository.FanScenarioRepository) *AirStoppageService {
	return &AirStoppageService{stoppages: stoppages, edges: edges, nodes: nodes, scenarios: scenarios}
}

func (s *AirStoppageService) List(ctx context.Context, query dto.StoppageListQuery) ([]model.AirStoppage, int64, int, int, error) {
	page, pageSize := normalizePage(query.Page, query.PageSize)
	if query.Status != "" && !constants.ValidStoppageStatus(query.Status) {
		return nil, 0, page, pageSize, api.BadRequest("INVALID_STOPPAGE_STATUS", "停风状态筛选值无效", nil)
	}
	items, total, err := s.stoppages.List(ctx, page, pageSize, query.Status)
	if err != nil {
		return nil, 0, page, pageSize, mapRepositoryError(err, stoppageEntity)
	}
	return items, total, page, pageSize, nil
}

func (s *AirStoppageService) Preview(ctx context.Context, edgeID uint) (*dto.StoppageEvaluation, error) {
	if err := s.checkRegistrable(ctx, edgeID); err != nil {
		return nil, err
	}
	return s.evaluate(ctx, edgeID)
}

func (s *AirStoppageService) Create(ctx context.Context, input dto.CreateAirStoppageRequest, actor Actor) (*model.AirStoppage, error) {
	if err := s.checkRegistrable(ctx, input.EdgeID); err != nil {
		return nil, err
	}
	plannedRestore, err := time.Parse(time.RFC3339, strings.TrimSpace(input.PlannedRestoreAt))
	if err != nil {
		return nil, api.BadRequest("INVALID_RESTORE_TIME", "恢复时间必须是 RFC 3339 格式的时间", nil)
	}
	if !plannedRestore.After(time.Now().UTC()) {
		return nil, api.BadRequest("RESTORE_TIME_NOT_FUTURE", "恢复时间必须晚于当前时间", nil)
	}
	evaluation, err := s.evaluate(ctx, input.EdgeID)
	if err != nil {
		return nil, err
	}
	if len(evaluation.ZeroAirWorkfaces) > 0 {
		return nil, api.Unprocessable("STOPPAGE_ZERO_AIR", zeroAirMessage(evaluation.ZeroAirWorkfaces), evaluation.ZeroAirWorkfaces)
	}
	notes := make(map[uint]string, len(input.Arrangements))
	for _, arrangement := range input.Arrangements {
		notes[arrangement.NodeID] = strings.TrimSpace(arrangement.Note)
	}
	missing := make([]dto.WorkfaceAirDeficit, 0)
	for _, deficit := range evaluation.Deficits {
		if utf8.RuneCountInString(notes[deficit.NodeID]) < minNoteRunes {
			missing = append(missing, deficit)
		}
	}
	if len(missing) > 0 {
		codes := make([]string, 0, len(missing))
		for _, deficit := range missing {
			codes = append(codes, deficit.Code)
		}
		return nil, api.Unprocessable("STOPPAGE_ARRANGEMENT_REQUIRED",
			fmt.Sprintf("工作面 %s 停风后风量低于需风量，必须写清本次停风安排", strings.Join(codes, "、")), missing)
	}
	for i := range evaluation.Deficits {
		evaluation.Deficits[i].Arrangement = notes[evaluation.Deficits[i].NodeID]
	}
	stoppage := &model.AirStoppage{
		EdgeID: input.EdgeID, Reason: strings.TrimSpace(input.Reason),
		PlannedRestoreAt: plannedRestore.UTC(), Status: string(constants.StoppageStatusStopping),
		EvaluationJSON: mustJSON(evaluation), CreatedBy: actor.ID,
	}
	audit := actor.Audit("air_stoppage.created", "air_stoppage")
	audit.Metadata = string(mustJSON(map[string]interface{}{
		"edge_id": input.EdgeID, "planned_restore_at": plannedRestore.UTC(),
		"scenario_id": evaluation.ScenarioID, "included_edge_ids": evaluation.IncludedEdgeIDs,
		"deficit_workfaces": len(evaluation.Deficits), "edge_losses": len(evaluation.EdgeLosses),
	}))
	if err := s.stoppages.Create(ctx, stoppage, audit); err != nil {
		return nil, mapRepositoryError(err, stoppageEntity)
	}
	created, err := s.stoppages.Find(ctx, stoppage.ID)
	if err != nil {
		return nil, mapRepositoryError(err, stoppageEntity)
	}
	return created, nil
}

func (s *AirStoppageService) Restore(ctx context.Context, id uint, actor Actor) (*model.AirStoppage, error) {
	stoppage, err := s.stoppages.Find(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err, stoppageEntity)
	}
	if stoppage.Status != string(constants.StoppageStatusStopping) {
		return nil, api.Conflict("STOPPAGE_NOT_ACTIVE", "该停风登记已恢复，不能重复操作")
	}
	audit := actor.Audit("air_stoppage.restored", "air_stoppage")
	audit.Metadata = string(mustJSON(map[string]interface{}{"edge_id": stoppage.EdgeID}))
	updated, err := s.stoppages.Restore(ctx, id, actor.ID, time.Now().UTC(), audit)
	if err != nil {
		return nil, mapRepositoryError(err, stoppageEntity)
	}
	return updated, nil
}

func (s *AirStoppageService) checkRegistrable(ctx context.Context, edgeID uint) error {
	edge, err := s.edges.Find(ctx, edgeID)
	if err != nil {
		return mapRepositoryError(err, airwayEdgeEntity)
	}
	if !edge.Enabled {
		return api.Conflict("STOPPAGE_EDGE_DISABLED", "停用巷道不能登记停风")
	}
	active, err := s.stoppages.HasActiveForEdge(ctx, edgeID)
	if err != nil {
		return mapRepositoryError(err, stoppageEntity)
	}
	if active {
		return api.Conflict("STOPPAGE_ALREADY_ACTIVE", "该巷道已有未恢复的停风登记，不能重复提交")
	}
	return nil
}

func (s *AirStoppageService) evaluate(ctx context.Context, edgeID uint) (*dto.StoppageEvaluation, error) {
	scenario, err := s.scenarios.LatestApproved(ctx)
	if err != nil {
		return nil, api.Conflict("NO_APPROVED_SCENARIO", "没有已批准的风机方案，无法评估停风影响")
	}
	nodes, err := s.nodes.AllActive(ctx)
	if err != nil {
		return nil, mapRepositoryError(err, "通风节点")
	}
	edges, err := s.edges.AllEnabled(ctx)
	if err != nil {
		return nil, mapRepositoryError(err, airwayEdgeEntity)
	}
	activeIDs, err := s.stoppages.ActiveEdgeIDs(ctx)
	if err != nil {
		return nil, mapRepositoryError(err, stoppageEntity)
	}
	included := append([]uint(nil), activeIDs...)
	found := false
	for _, id := range included {
		if id == edgeID {
			found = true
			break
		}
	}
	if !found {
		included = append(included, edgeID)
	}
	evaluation := evaluateStoppageImpact(*scenario, nodes, edges, included)
	return &evaluation, nil
}

func zeroAirMessage(zeros []dto.ZeroAirWorkface) string {
	parts := make([]string, 0, len(zeros))
	for _, zero := range zeros {
		cut := "风路被既有关闭风门或网络问题切断"
		if len(zero.CutEdgeCodes) > 0 {
			cut = fmt.Sprintf("巷道 %s 切断风路", strings.Join(zero.CutEdgeCodes, "、"))
		}
		parts = append(parts, fmt.Sprintf("工作面 %s 将完全失风（%s）", zero.Code, cut))
	}
	return "登记后" + strings.Join(parts, "；") + "，一点风都保不住，已驳回"
}

// applyStoppageOverlay 返回巷道副本，登记停风的巷道在推演中按风门关闭计算，不改动数据库原状态。
func applyStoppageOverlay(edges []model.AirwayEdge, stoppageEdgeIDs []uint) []model.AirwayEdge {
	if len(stoppageEdgeIDs) == 0 {
		return edges
	}
	stopped := make(map[uint]bool, len(stoppageEdgeIDs))
	for _, id := range stoppageEdgeIDs {
		stopped[id] = true
	}
	overlaid := make([]model.AirwayEdge, len(edges))
	copy(overlaid, edges)
	for i := range overlaid {
		if stopped[overlaid[i].ID] {
			overlaid[i].DoorState = string(constants.DoorStateClosed)
		}
	}
	return overlaid
}

// evaluateStoppageImpact 对比无停风与全部停风（已有 + 本次）两种推演，输出失风证据。
func evaluateStoppageImpact(scenario model.FanScenario, nodes []model.VentilationNode, edges []model.AirwayEdge, stoppageEdgeIDs []uint) dto.StoppageEvaluation {
	included := append([]uint(nil), stoppageEdgeIDs...)
	sort.Slice(included, func(i, j int) bool { return included[i] < included[j] })
	evaluation := dto.StoppageEvaluation{
		ScenarioID: scenario.ID, IncludedEdgeIDs: included,
		Deficits: []dto.WorkfaceAirDeficit{}, EdgeLosses: []dto.EdgeAirLoss{}, ZeroAirWorkfaces: []dto.ZeroAirWorkface{},
	}
	normal := solveNetwork(scenario, nodes, edges)
	stopped := solveNetwork(scenario, nodes, applyStoppageOverlay(edges, included))
	evaluation.SolverStatus = string(stopped.Status)
	normalThrough := nodeThroughFlows(edges, normal.Flows)
	stoppedThrough := nodeThroughFlows(edges, stopped.Flows)
	edgeCodes := make(map[uint]string, len(edges))
	for _, edge := range edges {
		edgeCodes[edge.ID] = edge.Code
	}
	for _, node := range nodes {
		if node.NodeType != string(constants.NodeTypeWorkface) || node.RequiredAirflowM3S <= 0 {
			continue
		}
		cutIDs := findAirCutEdges(nodes, edges, included, node.ID)
		if cutIDs != nil {
			cutCodes := make([]string, 0, len(cutIDs))
			for _, id := range cutIDs {
				cutCodes = append(cutCodes, edgeCodes[id])
			}
			evaluation.ZeroAirWorkfaces = append(evaluation.ZeroAirWorkfaces, dto.ZeroAirWorkface{
				NodeID: node.ID, Code: node.Code, CutEdgeIDs: cutIDs, CutEdgeCodes: cutCodes,
			})
			continue
		}
		available := stoppedThrough[node.ID]
		if available < node.RequiredAirflowM3S {
			evaluation.Deficits = append(evaluation.Deficits, dto.WorkfaceAirDeficit{
				NodeID: node.ID, Code: node.Code, RequiredM3S: node.RequiredAirflowM3S,
				NormalM3S: round(normalThrough[node.ID], 4), StoppedM3S: round(available, 4),
				DeficitM3S: round(node.RequiredAirflowM3S-available, 4),
			})
		}
	}
	for _, edge := range edges {
		normalFlow := math.Abs(normal.Flows[edge.ID])
		stoppedFlow := math.Abs(stopped.Flows[edge.ID])
		if loss := normalFlow - stoppedFlow; loss > airLossEpsilon {
			evaluation.EdgeLosses = append(evaluation.EdgeLosses, dto.EdgeAirLoss{
				EdgeID: edge.ID, Code: edge.Code,
				NormalM3S: round(normalFlow, 4), StoppedM3S: round(stoppedFlow, 4), LossM3S: round(loss, 4),
			})
		}
	}
	sort.Slice(evaluation.Deficits, func(i, j int) bool { return evaluation.Deficits[i].NodeID < evaluation.Deficits[j].NodeID })
	sort.Slice(evaluation.ZeroAirWorkfaces, func(i, j int) bool {
		return evaluation.ZeroAirWorkfaces[i].NodeID < evaluation.ZeroAirWorkfaces[j].NodeID
	})
	sort.Slice(evaluation.EdgeLosses, func(i, j int) bool { return evaluation.EdgeLosses[i].EdgeID < evaluation.EdgeLosses[j].EdgeID })
	return evaluation
}

// nodeThroughFlows 与 evaluateRisks 的口径一致：节点可用风量取进、出两侧绝对流量的较大值。
func nodeThroughFlows(edges []model.AirwayEdge, flows map[uint]float64) map[uint]float64 {
	incoming := make(map[uint]float64, len(edges))
	outgoing := make(map[uint]float64, len(edges))
	for _, edge := range edges {
		flow := flows[edge.ID]
		if flow < 0 {
			incoming[edge.FromNodeID] += -flow
			outgoing[edge.ToNodeID] += -flow
		} else {
			outgoing[edge.FromNodeID] += flow
			incoming[edge.ToNodeID] += flow
		}
	}
	through := make(map[uint]float64, len(incoming)+len(outgoing))
	for id, value := range incoming {
		through[id] = math.Max(value, outgoing[id])
	}
	for id, value := range outgoing {
		if _, ok := through[id]; !ok {
			through[id] = math.Max(value, incoming[id])
		}
	}
	return through
}

// findAirCutEdges 判断停风后工作面是否从进风边界不可达（一点风都保不住），
// 并找出切断风路的停风巷道：逐条恢复后能重新到达的即为切断点。
// 返回 nil 表示风路仍然连通；空切片表示不可达但责任在既有关闭风门或网络问题。
func findAirCutEdges(nodes []model.VentilationNode, edges []model.AirwayEdge, stoppageEdgeIDs []uint, workfaceID uint) []uint {
	stopped := make(map[uint]bool, len(stoppageEdgeIDs))
	for _, id := range stoppageEdgeIDs {
		stopped[id] = true
	}
	adjacency := func(ignoreStoppage bool, restoreID uint) map[uint][]uint {
		result := make(map[uint][]uint, len(nodes))
		for _, edge := range edges {
			if !edge.Enabled || edge.DoorState == string(constants.DoorStateClosed) {
				continue
			}
			if !ignoreStoppage && stopped[edge.ID] && edge.ID != restoreID {
				continue
			}
			result[edge.FromNodeID] = append(result[edge.FromNodeID], edge.ToNodeID)
		}
		return result
	}
	reachable := func(graph map[uint][]uint) bool {
		seen := make(map[uint]bool, len(nodes))
		queue := make([]uint, 0, len(nodes))
		for _, node := range nodes {
			if node.NodeType == string(constants.NodeTypeIntake) {
				queue = append(queue, node.ID)
			}
		}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if seen[id] {
				continue
			}
			seen[id] = true
			if id == workfaceID {
				return true
			}
			next := append([]uint(nil), graph[id]...)
			sort.Slice(next, func(i, j int) bool { return next[i] < next[j] })
			queue = append(queue, next...)
		}
		return false
	}
	if reachable(adjacency(false, 0)) {
		return nil
	}
	if !reachable(adjacency(true, 0)) {
		return []uint{}
	}
	cuts := make([]uint, 0, len(stoppageEdgeIDs))
	for _, id := range stoppageEdgeIDs {
		if reachable(adjacency(false, id)) {
			cuts = append(cuts, id)
		}
	}
	if len(cuts) == 0 {
		cuts = append(cuts, stoppageEdgeIDs...)
	}
	return cuts
}
