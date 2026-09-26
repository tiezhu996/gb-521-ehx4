package model

import (
	"time"

	"gorm.io/datatypes"
)

// VentilationStoppage 记录一次检修停风登记。登记期间 EdgeID 指向的巷道在风量
// 推演中按风门关闭处理，但巷道自身的 door_state 不会被改写；恢复后推演自动回到
// 登记前状态。ActiveEdgeID 仅在登记有效（active）时等于 EdgeID，数据库唯一索引
// 保证同一条巷道同时只能有一条未结束的停风。
type VentilationStoppage struct {
	ID                uint           `gorm:"primaryKey" json:"id"`
	EdgeID            uint           `gorm:"not null;index" json:"edge_id"`
	Status            string         `gorm:"size:20;not null;index;check:status IN ('active','recovered')" json:"status"`
	Reason            string         `gorm:"size:300;not null" json:"reason"`
	PlannedRestoreAt  time.Time      `gorm:"not null" json:"planned_restore_at"`
	ActualRestoreAt   *time.Time     `json:"actual_restore_at"`
	BaselineDoorState string         `gorm:"size:20;not null" json:"baseline_door_state"`
	ImpactJSON        datatypes.JSON `gorm:"type:jsonb;not null" json:"impact_json"`
	ArrangementJSON   datatypes.JSON `gorm:"type:jsonb;not null" json:"arrangement_json"`
	RegisteredBy      uint           `gorm:"not null;index" json:"registered_by"`
	RecoveredBy       *uint          `gorm:"index" json:"recovered_by"`
	ActiveEdgeID      *uint          `gorm:"uniqueIndex" json:"-"`
	Edge              AirwayEdge     `gorm:"foreignKey:EdgeID" json:"edge,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

func (VentilationStoppage) TableName() string { return "ventilation_stoppages" }
