package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"mine-ventilation-network-simulator/backend/internal/constants"
	"mine-ventilation-network-simulator/backend/internal/dto"
	"mine-ventilation-network-simulator/backend/internal/model"
	"mine-ventilation-network-simulator/backend/internal/repository"
	"mine-ventilation-network-simulator/backend/pkg/api"
)

// stoppageZeroAirThreshold 工作面风量低于该值视为“一点风都保不住”，与
// AIR-PATH-004 关键路径无有效风量阈值保持一致。
const stoppageZeroAirThreshold = 0.01

type VentilationStoppageService struct {
	stoppages *repository.VentilationStoppageRepository
	edges     *repository.AirwayEdgeRepository
	nodes     *repository.VentilationNodeRepository
	scenarios *repository.FanScenarioRepository
}

func NewVentilationStoppageService(stoppages *repository.VentilationStoppageRepository, edges *repository.AirwayEdgeRepository, nodes *repository.VentilationNodeRepository, scenarios *repository.FanScenarioRepository) *VentilationStoppageService {
	return &VentilationStoppageService{stoppages: stoppages, edges: edges, nodes: nodes, scenarios: scenarios}
}

func (s *VentilationStoppageService) List(ctx context.Context, query dto.StoppageListQuery) ([]model.VentilationStoppage, int64, int, int, error) {
	page, pageSize := normalizePage(query.Page, query.PageSize)
	if query.Status != "" && !constants.ValidStoppageStatus(query.Status) {
		return nil, 0, page, pageSize, api.BadRequest("INVALID_STOPPAGE_STATUS", "停风状态筛选值只能是 active 或 recovered", nil)
	}
	items, total, err := s.stoppages.List(ctx, page, pageSize, query.Status, query.EdgeID)
	if err != nil {
		return nil, 0, page, pageSize, mapRepositoryError(err, "停风登记")
	}
	return items, total, page, pageSize, nil
}

func (s *VentilationStoppageService) Get(ctx context.Context, id uint) (*model.VentilationStoppage, error) {
	item, err := s.stoppages.Find(ctx, id)
	return item, mapRepositoryError(err, "停风登记")
}

// Preview 不落库，只给前端展示“已有停风 + 候选巷道”一起关闭后的影响。
func (s *VentilationStoppageService) Preview(ctx context.Context, input dto.PreviewStoppageRequest) (*dto.StoppageAssessment, error) {
	if _, err := s.resolveCandidateEdge(ctx, input.EdgeID); err != nil {
		return nil, err
	}
	if !input.PlannedRestoreAt.UTC().After(time.Now().UTC()) {
		return nil, api.BadRequest("RESTORE_TIME_MUST_BE_FUTURE", "计划恢复时间必须晚于当前时间", nil)
	}
	return s.assess(ctx, input.EdgeID)
}

func (s *VentilationStoppageService) Create(ctx context.Context, input dto.CreateStoppageRequest, actor Actor) (*model.VentilationStoppage, error) {
	reason := strings.TrimSpace(input.Reason)
	if len([]rune(reason)) < 4 {
		return nil, api.BadRequest("STOPPAGE_REASON_REQUIRED", "停风原因至少需要 4 个字符", nil)
	}
	restoreAt := input.PlannedRestoreAt.UTC()
	if !restoreAt.After(time.Now().UTC()) {
		return nil, api.BadRequest("RESTORE_TIME_MUST_BE_FUTURE", "计划恢复时间必须晚于当前时间", nil)
	}
	edge, err := s.resolveCandidateEdge(ctx, input.EdgeID)
	if err != nil {
		return nil, err
	}
	if existing, findErr := s.stoppages.FindActiveByEdge(ctx, edge.ID); findErr == nil {
		return nil, api.Conflict("STOPPAGE_ALREADY_ACTIVE", fmt.Sprintf("巷道 %s 已有未结束的停风登记 #%d，恢复前不能再次登记", edge.Code, existing.ID))
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, mapRepositoryError(findErr, "停风登记")
	}

	assessment, err := s.assess(ctx, edge.ID)
	if err != nil {
		return nil, err
	}
	if len(assessment.ZeroAirWorkfaces) > 0 {
		named := make([]string, 0, len(assessment.ZeroAirWorkfaces))
		for _, zero := range assessment.ZeroAirWorkfaces {
			if zero.CuttingCode != "" {
				named = append(named, fmt.Sprintf("工作面 %s 无风：巷道 %s 切断了风路", zero.WorkfaceCode, zero.CuttingCode))
			} else {
				named = append(named, fmt.Sprintf("工作面 %s 无风：%s", zero.WorkfaceCode, zero.Reason))
			}
		}
		return nil, api.Unprocessable("WORKFACE_ZERO_AIRFLOW", "登记被驳回："+strings.Join(named, "；"), assessment.ZeroAirWorkfaces)
	}

	needed := make(map[uint]bool, len(assessment.WorkfaceImpacts))
	for _, impact := range assessment.WorkfaceImpacts {
		if impact.ArrangementNeeded {
			needed[impact.WorkfaceNodeID] = true
		}
	}
	arrangements := make(map[uint]string, len(input.Arrangements))
	for _, item := range input.Arrangements {
		text := strings.TrimSpace(item.Arrangement)
		if len([]rune(text)) < 4 {
			return nil, api.BadRequest("ARRANGEMENT_TEXT_REQUIRED", "每个仍有风但低于需风量的工作面都必须写明不少于 4 个字符的停风安排", map[string]uint{"workface_node_id": item.WorkfaceNodeID})
		}
		if !needed[item.WorkfaceNodeID] {
			return nil, api.BadRequest("ARRANGEMENT_WORKFACE_MISMATCH", "只能为评估结果中低于需风量但仍有风的工作面填写安排", map[string]uint{"workface_node_id": item.WorkfaceNodeID})
		}
		if _, duplicated := arrangements[item.WorkfaceNodeID]; duplicated {
			return nil, api.BadRequest("ARRANGEMENT_DUPLICATED", "同一个工作面只能填写一条停风安排", map[string]uint{"workface_node_id": item.WorkfaceNodeID})
		}
		arrangements[item.WorkfaceNodeID] = text
	}
	for nodeID := range needed {
		if _, ok := arrangements[nodeID]; !ok {
			return nil, api.BadRequest("ARRANGEMENT_REQUIRED", "仍有风但低于需风量的工作面缺少停风安排，提交前必须逐条写明", map[string]uint{"workface_node_id": nodeID})
		}
	}

	activeEdgeID := edge.ID
	stoppage := &model.VentilationStoppage{
		EdgeID: edge.ID, Status: string(constants.StoppageStatusActive), Reason: reason,
		PlannedRestoreAt: restoreAt, BaselineDoorState: edge.DoorState,
		ImpactJSON: mustJSON(assessment), ArrangementJSON: mustJSON(arrangements),
		RegisteredBy: actor.ID, ActiveEdgeID: &activeEdgeID,
	}
	audit := actor.Audit("ventilation_stoppage.registered", "ventilation_stoppage")
	audit.Metadata = string(mustJSON(map[string]interface{}{
		"edge_id": edge.ID, "edge_code": edge.Code, "planned_restore_at": restoreAt,
		"combined_edge_ids": assessment.CombinedEdgeIDs, "shortfall_workfaces": len(assessment.WorkfaceImpacts),
	}))
	if err := s.stoppages.Create(ctx, stoppage, audit); err != nil {
		return nil, mapRepositoryError(err, "停风登记")
	}
	return s.Get(ctx, stoppage.ID)
}

func (s *VentilationStoppageService) Recover(ctx context.Context, id uint, note string, actor Actor) (*model.VentilationStoppage, error) {
	existing, err := s.stoppages.Find(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err, "停风登记")
	}
	if existing.Status != string(constants.StoppageStatusActive) {
		return nil, api.Conflict("STOPPAGE_ALREADY_RECOVERED", fmt.Sprintf("停风登记 #%d 已恢复，不能重复恢复", id))
	}
	audit := actor.Audit("ventilation_stoppage.recovered", "ventilation_stoppage")
	audit.Metadata = string(mustJSON(map[string]interface{}{
		"edge_id": existing.EdgeID, "baseline_door_state": existing.BaselineDoorState,
		"note": strings.TrimSpace(note),
		"rule": "恢复后推演按巷道登记前风门状态计算，未改写网络模型",
	}))
	updated, err := s.stoppages.Recover(ctx, id, actor.ID, audit)
	if err != nil {
		return nil, mapRepositoryError(err, "停风恢复")
	}
	return updated, nil
}

// resolveCandidateEdge 校验巷道存在、启用且当前风门不是关闭状态：在模型中已经
// 关门的巷道不属于检修停风登记范围，应直接维护巷道参数而不是补登记。
func (s *VentilationStoppageService) resolveCandidateEdge(ctx context.Context, edgeID uint) (*model.AirwayEdge, error) {
	edges, err := s.edges.AllEnabled(ctx)
	if err != nil {
		return nil, mapRepositoryError(err, "巷道边")
	}
	for i := range edges {
		if edges[i].ID == edgeID {
			if edges[i].DoorState == string(constants.DoorStateClosed) {
				return nil, api.BadRequest("EDGE_ALREADY_CLOSED", "该巷道在网络模型中风门已关闭，应直接维护巷道参数而不是补停风登记", map[string]uint{"edge_id": edgeID})
			}
			return &edges[i], nil
		}
	}
	return nil, api.NotFound("可登记的启用巷道")
}

// assess 用最近批准方案推演当前状态（所有未结束停风视为关门）以及叠加本次候选
// 巷道关门后的状态，输出低于需风量的工作面、无风工作面（含切断风路的巷道）和
// 相关巷道失风量。整个过程不改写任何模型数据。
func (s *VentilationStoppageService) assess(ctx context.Context, newEdgeID uint) (*dto.StoppageAssessment, error) {
	scenario, err := s.scenarios.LatestApproved(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, api.Conflict("NO_APPROVED_SCENARIO", "没有已批准风机方案，无法推演停风影响；请先完成方案批准")
		}
		return nil, mapRepositoryError(err, "风机方案")
	}
	nodes, err := s.nodes.AllActive(ctx)
	if err != nil {
		return nil, mapRepositoryError(err, "通风节点")
	}
	edges, err := s.edges.AllEnabled(ctx)
	if err != nil {
		return nil, mapRepositoryError(err, "巷道边")
	}
	active, err := s.stoppages.AllActive(ctx)
	if err != nil {
		return nil, mapRepositoryError(err, "停风登记")
	}

	activeIDs := make(map[uint]bool, len(active))
	for _, stoppage := range active {
		if _, ok := findEdge(edges, stoppage.EdgeID); ok {
			activeIDs[stoppage.EdgeID] = true
		}
	}
	combinedIDs := map[uint]bool{newEdgeID: true}
	for id := range activeIDs {
		combinedIDs[id] = true
	}

	baselineResult := solveNetwork(*scenario, nodes, edgesWithClosedDoors(edges, activeIDs))
	projectedResult := solveNetwork(*scenario, nodes, edgesWithClosedDoors(edges, combinedIDs))
	baselineAir := workfaceAirflows(edges, baselineResult.Flows)
	projectedAir := workfaceAirflows(edges, projectedResult.Flows)
	graph := newAirflowGraph(nodes, edges)

	assessment := &dto.StoppageAssessment{
		ScenarioID: scenario.ID, SolverStatus: string(projectedResult.Status),
		NewEdgeID: newEdgeID, ActiveEdgeIDs: sortedUintKeys(activeIDs),
		CombinedEdgeIDs: sortedUintKeys(combinedIDs), CanRegister: true,
		WorkfaceImpacts:  []dto.StoppageWorkfaceImpact{},
		ZeroAirWorkfaces: []dto.StoppageZeroAirWorkface{},
		EdgeFlowLosses:   []dto.StoppageEdgeFlowLoss{},
	}

	for _, node := range nodes {
		if node.NodeType != string(constants.NodeTypeWorkface) {
			continue
		}
		before := round(baselineAir[node.ID], 3)
		after := round(projectedAir[node.ID], 3)
		// 无风判定以风路是否被切断为主、解算风量为辅：求解器残差容差可能
		// 略高于无风阈值，风路已断时残差风量只是数值噪声。
		baselineZero := !graph.ventilated(node.ID, activeIDs) || before < stoppageZeroAirThreshold
		projectedZero := !graph.ventilated(node.ID, combinedIDs) || after < stoppageZeroAirThreshold
		if projectedZero {
			cuttingEdgeID, cuttingCode, reason := identifyCuttingEdge(node.ID, graph, activeIDs, newEdgeID, combinedIDs, baselineZero)
			assessment.ZeroAirWorkfaces = append(assessment.ZeroAirWorkfaces, dto.StoppageZeroAirWorkface{
				WorkfaceNodeID: node.ID, WorkfaceCode: node.Code,
				CuttingEdgeID: cuttingEdgeID, CuttingCode: cuttingCode, Reason: reason,
			})
			continue
		}
		if node.RequiredAirflowM3S > 0 && after < node.RequiredAirflowM3S {
			assessment.WorkfaceImpacts = append(assessment.WorkfaceImpacts, dto.StoppageWorkfaceImpact{
				WorkfaceNodeID: node.ID, WorkfaceCode: node.Code,
				RequiredAirflow: round(node.RequiredAirflowM3S, 3), BaselineAirflow: before, ProjectedAirflow: after,
				Shortfall: round(node.RequiredAirflowM3S-after, 3), ArrangementNeeded: true,
			})
		}
	}

	for _, edge := range edges {
		before := round(baselineResult.Flows[edge.ID], 3)
		after := round(projectedResult.Flows[edge.ID], 3)
		loss := round(math.Abs(before)-math.Abs(after), 3)
		if loss < 0.01 && !combinedIDs[edge.ID] {
			continue
		}
		assessment.EdgeFlowLosses = append(assessment.EdgeFlowLosses, dto.StoppageEdgeFlowLoss{
			EdgeID: edge.ID, EdgeCode: edge.Code, BaselineFlow: before, ProjectedFlow: after,
			FlowLoss: math.Max(0, loss), Closed: combinedIDs[edge.ID], AlreadyStopped: activeIDs[edge.ID],
		})
	}
	sort.Slice(assessment.WorkfaceImpacts, func(i, j int) bool {
		return assessment.WorkfaceImpacts[i].WorkfaceNodeID < assessment.WorkfaceImpacts[j].WorkfaceNodeID
	})
	sort.Slice(assessment.ZeroAirWorkfaces, func(i, j int) bool {
		return assessment.ZeroAirWorkfaces[i].WorkfaceNodeID < assessment.ZeroAirWorkfaces[j].WorkfaceNodeID
	})
	sort.Slice(assessment.EdgeFlowLosses, func(i, j int) bool {
		if assessment.EdgeFlowLosses[i].FlowLoss == assessment.EdgeFlowLosses[j].FlowLoss {
			return assessment.EdgeFlowLosses[i].EdgeID < assessment.EdgeFlowLosses[j].EdgeID
		}
		return assessment.EdgeFlowLosses[i].FlowLoss > assessment.EdgeFlowLosses[j].FlowLoss
	})
	if len(assessment.ZeroAirWorkfaces) > 0 {
		assessment.CanRegister = false
	}
	return assessment, nil
}

// identifyCuttingEdge 找出切断该工作面风路的巷道：先逐条“放开”被关巷道做
// 连通性验证（本次新增巷道优先），再回退到放开全部停风后的最短风路定位。
func identifyCuttingEdge(workfaceID uint, graph airflowGraph, activeIDs map[uint]bool, newEdgeID uint, combinedIDs map[uint]bool, baselineZero bool) (uint, string, string) {
	// 风路仍连通但解算风量近乎为零时，本次新增的关门巷道即为切断原因。
	if graph.ventilated(workfaceID, combinedIDs) {
		if edge, ok := graph.byID[newEdgeID]; ok {
			return newEdgeID, edge.Code, "停风后风路仍连通，但该工作面计算风量低于无风阈值"
		}
		return 0, "", "该工作面计算风量低于无风阈值，但无法定位单一关门巷道"
	}
	// 登记前就已经无风：说明既有停风已经切断风路，同样必须驳回。
	if baselineZero {
		if !graph.ventilated(workfaceID, activeIDs) {
			if id, code, ok := graph.removalTestCut(workfaceID, activeIDs, sortedUintKeys(activeIDs)); ok {
				return id, code, "该工作面在本次登记前已无风，该既有停风巷道切断了风路"
			}
		}
		return 0, "", "该工作面在本次登记前已无有效风量，请先恢复既有停风"
	}
	// 优先验证本次新增巷道：单独放开它后风路恢复，即由它切断。
	if id, code, ok := graph.removalTestCut(workfaceID, combinedIDs, []uint{newEdgeID}); ok {
		return id, code, "该巷道关闭后工作面风路被切断"
	}
	if id, code, ok := graph.removalTestCut(workfaceID, combinedIDs, sortedUintKeys(combinedIDs)); ok {
		return id, code, "该巷道关闭后工作面风路被切断"
	}
	if id, code, ok := graph.shortestPathCut(workfaceID, combinedIDs); ok {
		return id, code, "该巷道位于工作面唯一风路上，关闭后切断风路"
	}
	return 0, "", "工作面风路被切断，但无法定位单一巷道"
}

// airflowGraph 是基于当前启用巷道与节点类型构建的有向风路图，关门集合只在
// 遍历时生效，不修改任何模型数据。
type airflowGraph struct {
	edges    []model.AirwayEdge
	byID     map[uint]model.AirwayEdge
	intakes  []uint
	exhausts map[uint]bool
}

type edgeRef struct {
	to     uint
	edgeID uint
}

func newAirflowGraph(nodes []model.VentilationNode, edges []model.AirwayEdge) airflowGraph {
	graph := airflowGraph{edges: edges, byID: make(map[uint]model.AirwayEdge, len(edges)), exhausts: make(map[uint]bool)}
	for _, edge := range edges {
		graph.byID[edge.ID] = edge
	}
	for _, node := range nodes {
		switch constants.NodeType(node.NodeType) {
		case constants.NodeTypeIntake:
			graph.intakes = append(graph.intakes, node.ID)
		case constants.NodeTypeExhaust:
			graph.exhausts[node.ID] = true
		}
	}
	sort.Slice(graph.intakes, func(i, j int) bool { return graph.intakes[i] < graph.intakes[j] })
	return graph
}

func (g airflowGraph) adjacency(closed map[uint]bool) map[uint][]edgeRef {
	out := make(map[uint][]edgeRef)
	for _, edge := range g.edges {
		if closed[edge.ID] {
			continue
		}
		out[edge.FromNodeID] = append(out[edge.FromNodeID], edgeRef{to: edge.ToNodeID, edgeID: edge.ID})
	}
	return out
}

// ventilated 判断工作面在给定关门集合下是否同时具备进风口来风通路和通向
// 回风口的排风通路，两者缺一即视为无风。
func (g airflowGraph) ventilated(workfaceID uint, closed map[uint]bool) bool {
	out := g.adjacency(closed)
	if !reachableFrom(g.intakes, workfaceID, out) {
		return false
	}
	return reachableToAny(workfaceID, g.exhausts, out)
}

func reachableFrom(sources []uint, target uint, out map[uint][]edgeRef) bool {
	seen := make(map[uint]bool, len(out))
	queue := append([]uint(nil), sources...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == target {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		for _, ref := range out[current] {
			queue = append(queue, ref.to)
		}
	}
	return false
}

func reachableToAny(start uint, targets map[uint]bool, out map[uint][]edgeRef) bool {
	if targets[start] {
		return true
	}
	seen := map[uint]bool{start: true}
	queue := []uint{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, ref := range out[current] {
			if seen[ref.to] {
				continue
			}
			if targets[ref.to] {
				return true
			}
			seen[ref.to] = true
			queue = append(queue, ref.to)
		}
	}
	return false
}

// removalTestCut 按候选顺序逐条放开关闭巷道，若放开后工作面恢复通风，则该
// 巷道就是切断风路的巷道。
func (g airflowGraph) removalTestCut(workfaceID uint, closed map[uint]bool, candidates []uint) (uint, string, bool) {
	for _, candidate := range candidates {
		if !closed[candidate] {
			continue
		}
		trial := make(map[uint]bool, len(closed))
		for id := range closed {
			if id != candidate {
				trial[id] = true
			}
		}
		if g.ventilated(workfaceID, trial) {
			edge, ok := g.byID[candidate]
			return candidate, edge.Code, ok
		}
	}
	return 0, "", false
}

// shortestPathCut 在全部停风巷道都放开的图上找进风口→工作面、工作面→回风口
// 的最短有向风路，取风路上第一条被关闭的巷道。
func (g airflowGraph) shortestPathCut(workfaceID uint, closed map[uint]bool) (uint, string, bool) {
	out := g.adjacency(map[uint]bool{})
	if path, ok := shortestPathEdges(g.intakes, map[uint]bool{workfaceID: true}, out); ok {
		if id, code, found := firstClosedOnPath(path, closed, g.byID); found {
			return id, code, true
		}
	}
	if path, ok := shortestPathEdges([]uint{workfaceID}, g.exhausts, out); ok {
		if id, code, found := firstClosedOnPath(path, closed, g.byID); found {
			return id, code, true
		}
	}
	return 0, "", false
}

func firstClosedOnPath(path []uint, closed map[uint]bool, byID map[uint]model.AirwayEdge) (uint, string, bool) {
	for _, edgeID := range path {
		if closed[edgeID] {
			edge, ok := byID[edgeID]
			return edgeID, edge.Code, ok
		}
	}
	return 0, "", false
}

// shortestPathEdges 用 BFS 返回从任一 source 到任一 target 的最短有向路径上
// 的巷道 ID 序列。
func shortestPathEdges(sources []uint, targets map[uint]bool, out map[uint][]edgeRef) ([]uint, bool) {
	if len(sources) == 0 || len(targets) == 0 {
		return nil, false
	}
	prevNode := make(map[uint]uint)
	prevEdge := make(map[uint]uint)
	queued := make(map[uint]bool, len(sources))
	queue := append([]uint(nil), sources...)
	for _, source := range sources {
		queued[source] = true
		if targets[source] {
			return []uint{}, true
		}
	}
	end := uint(0)
	found := false
	for len(queue) > 0 && !found {
		current := queue[0]
		queue = queue[1:]
		refs := append([]edgeRef(nil), out[current]...)
		sort.Slice(refs, func(i, j int) bool { return refs[i].edgeID < refs[j].edgeID })
		for _, ref := range refs {
			if queued[ref.to] {
				continue
			}
			queued[ref.to] = true
			prevNode[ref.to] = current
			prevEdge[ref.to] = ref.edgeID
			if targets[ref.to] {
				end = ref.to
				found = true
				break
			}
			queue = append(queue, ref.to)
		}
	}
	if !found {
		return nil, false
	}
	path := []uint{}
	for current := end; ; {
		edgeID, ok := prevEdge[current]
		if !ok {
			break
		}
		path = append([]uint{edgeID}, path...)
		current = prevNode[current]
	}
	return path, true
}

func findEdge(edges []model.AirwayEdge, id uint) (model.AirwayEdge, bool) {
	for _, edge := range edges {
		if edge.ID == id {
			return edge, true
		}
	}
	return model.AirwayEdge{}, false
}

func edgesWithClosedDoors(edges []model.AirwayEdge, closedIDs map[uint]bool) []model.AirwayEdge {
	clone := make([]model.AirwayEdge, len(edges))
	copy(clone, edges)
	for i := range clone {
		if closedIDs[clone[i].ID] {
			clone[i].DoorState = string(constants.DoorStateClosed)
		}
	}
	return clone
}

// workfaceAirflows 复用风险规则口径：取每个节点入流与出流绝对量的较大者。
func workfaceAirflows(edges []model.AirwayEdge, flows map[uint]float64) map[uint]float64 {
	incoming := make(map[uint]float64)
	outgoing := make(map[uint]float64)
	for _, edge := range edges {
		flow := flows[edge.ID]
		if flow < -0.01 {
			incoming[edge.FromNodeID] += -flow
			outgoing[edge.ToNodeID] += -flow
		} else {
			outgoing[edge.FromNodeID] += flow
			incoming[edge.ToNodeID] += flow
		}
	}
	available := make(map[uint]float64)
	for nodeID := range incoming {
		available[nodeID] = math.Max(incoming[nodeID], outgoing[nodeID])
	}
	for nodeID := range outgoing {
		if _, ok := available[nodeID]; !ok {
			available[nodeID] = math.Max(incoming[nodeID], outgoing[nodeID])
		}
	}
	return available
}

func sortedUintKeys(items map[uint]bool) []uint {
	keys := make([]uint, 0, len(items))
	for id := range items {
		keys = append(keys, id)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
