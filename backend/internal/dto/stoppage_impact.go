package dto

// StoppageWorkfaceImpact 描述一个工作面在“已有停风 + 本次停风”同时关闭后的风量变化。
type StoppageWorkfaceImpact struct {
	WorkfaceNodeID    uint    `json:"workface_node_id"`
	WorkfaceCode      string  `json:"workface_code"`
	RequiredAirflow   float64 `json:"required_airflow_m3s"`
	BaselineAirflow   float64 `json:"baseline_airflow_m3s"`
	ProjectedAirflow  float64 `json:"projected_airflow_m3s"`
	Shortfall         float64 `json:"shortfall_m3s"`
	ArrangementNeeded bool    `json:"arrangement_needed"`
}

// StoppageZeroAirWorkface 描述一点风都保不住、必须驳回登记的工作面。
type StoppageZeroAirWorkface struct {
	WorkfaceNodeID uint   `json:"workface_node_id"`
	WorkfaceCode   string `json:"workface_code"`
	CuttingEdgeID  uint   `json:"cutting_edge_id"`
	CuttingCode    string `json:"cutting_edge_code"`
	Reason         string `json:"reason"`
}

// StoppageEdgeFlowLoss 描述相关巷道相对当前状态的失风量。
type StoppageEdgeFlowLoss struct {
	EdgeID         uint    `json:"edge_id"`
	EdgeCode       string  `json:"edge_code"`
	BaselineFlow   float64 `json:"baseline_flow_m3s"`
	ProjectedFlow  float64 `json:"projected_flow_m3s"`
	FlowLoss       float64 `json:"flow_loss_m3s"`
	Closed         bool    `json:"closed"`
	AlreadyStopped bool    `json:"already_stopped"`
}

// StoppageAssessment 是提交时把所有在停风巷道一起算进去后的完整影响评估，
// 同时也是登记记录 impact_json 的落库结构。
type StoppageAssessment struct {
	ScenarioID       uint                      `json:"scenario_id"`
	SolverStatus     string                    `json:"solver_status"`
	CombinedEdgeIDs  []uint                    `json:"combined_edge_ids"`
	NewEdgeID        uint                      `json:"new_edge_id"`
	ActiveEdgeIDs    []uint                    `json:"active_edge_ids"`
	CanRegister      bool                      `json:"can_register"`
	WorkfaceImpacts  []StoppageWorkfaceImpact  `json:"workface_impacts"`
	ZeroAirWorkfaces []StoppageZeroAirWorkface `json:"zero_air_workfaces"`
	EdgeFlowLosses   []StoppageEdgeFlowLoss    `json:"edge_flow_losses"`
}
