package dto

type StoppageArrangementInput struct {
	NodeID uint   `json:"node_id" binding:"required"`
	Note   string `json:"note" binding:"required,min=4,max=300"`
}

type CreateAirStoppageRequest struct {
	EdgeID           uint                       `json:"edge_id" binding:"required"`
	Reason           string                     `json:"reason" binding:"required,min=4,max=400"`
	PlannedRestoreAt string                     `json:"planned_restore_at" binding:"required"`
	Arrangements     []StoppageArrangementInput `json:"arrangements" binding:"omitempty,max=50,dive"`
}

type PreviewAirStoppageRequest struct {
	EdgeID uint `json:"edge_id" binding:"required"`
}

type StoppageListQuery struct {
	Page     int    `form:"page" binding:"omitempty,gte=1"`
	PageSize int    `form:"page_size" binding:"omitempty,gte=1,lte=100"`
	Status   string `form:"status"`
}

type WorkfaceAirDeficit struct {
	NodeID      uint    `json:"node_id"`
	Code        string  `json:"code"`
	RequiredM3S float64 `json:"required_m3s"`
	NormalM3S   float64 `json:"normal_m3s"`
	StoppedM3S  float64 `json:"stopped_m3s"`
	DeficitM3S  float64 `json:"deficit_m3s"`
	Arrangement string  `json:"arrangement,omitempty"`
}

type EdgeAirLoss struct {
	EdgeID     uint    `json:"edge_id"`
	Code       string  `json:"code"`
	NormalM3S  float64 `json:"normal_m3s"`
	StoppedM3S float64 `json:"stopped_m3s"`
	LossM3S    float64 `json:"loss_m3s"`
}

type ZeroAirWorkface struct {
	NodeID       uint     `json:"node_id"`
	Code         string   `json:"code"`
	CutEdgeIDs   []uint   `json:"cut_edge_ids"`
	CutEdgeCodes []string `json:"cut_edge_codes"`
}

type StoppageEvaluation struct {
	ScenarioID       uint                 `json:"scenario_id"`
	SolverStatus     string               `json:"solver_status"`
	IncludedEdgeIDs  []uint               `json:"included_edge_ids"`
	Deficits         []WorkfaceAirDeficit `json:"deficits"`
	EdgeLosses       []EdgeAirLoss        `json:"edge_losses"`
	ZeroAirWorkfaces []ZeroAirWorkface    `json:"zero_air_workfaces"`
}
