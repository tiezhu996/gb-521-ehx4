package dto

import "time"

// PreviewStoppageRequest 只做落库前的影响推演，不写任何数据。
type PreviewStoppageRequest struct {
	EdgeID           uint      `json:"edge_id" binding:"required"`
	PlannedRestoreAt time.Time `json:"planned_restore_at" binding:"required" time_format:"rfc3339"`
}

// WorkfaceArrangement 是登记人对“仍有风但低于需风量”工作面填写的安排。
type WorkfaceArrangement struct {
	WorkfaceNodeID uint   `json:"workface_node_id" binding:"required"`
	Arrangement    string `json:"arrangement" binding:"required,min=4,max=300"`
}

// CreateStoppageRequest 登记一条检修停风。
type CreateStoppageRequest struct {
	EdgeID           uint                  `json:"edge_id" binding:"required"`
	Reason           string                `json:"reason" binding:"required,min=4,max=300"`
	PlannedRestoreAt time.Time             `json:"planned_restore_at" binding:"required" time_format:"rfc3339"`
	Arrangements     []WorkfaceArrangement `json:"arrangements" binding:"dive"`
}

type StoppageListQuery struct {
	Page     int    `form:"page" binding:"omitempty,gte=1"`
	PageSize int    `form:"page_size" binding:"omitempty,gte=1,lte=100"`
	Status   string `form:"status"`
	EdgeID   uint   `form:"edge_id"`
}

type RecoverStoppageRequest struct {
	Note string `json:"note" binding:"omitempty,max=300"`
}
