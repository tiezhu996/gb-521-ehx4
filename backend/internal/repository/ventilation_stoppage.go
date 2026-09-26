package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"mine-ventilation-network-simulator/backend/internal/model"
)

type VentilationStoppageRepository struct{ db *gorm.DB }

func NewVentilationStoppageRepository(db *gorm.DB) *VentilationStoppageRepository {
	return &VentilationStoppageRepository{db: db}
}

func (r *VentilationStoppageRepository) List(ctx context.Context, page, pageSize int, status string, edgeID uint) ([]model.VentilationStoppage, int64, error) {
	query := r.db.WithContext(ctx).Model(&model.VentilationStoppage{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if edgeID > 0 {
		query = query.Where("edge_id = ?", edgeID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count ventilation stoppages: %w", err)
	}
	var items []model.VentilationStoppage
	err := query.Preload("Edge").Preload("Edge.FromNode").Preload("Edge.ToNode").
		Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list ventilation stoppages: %w", err)
	}
	return items, total, nil
}

func (r *VentilationStoppageRepository) Find(ctx context.Context, id uint) (*model.VentilationStoppage, error) {
	var item model.VentilationStoppage
	if err := r.db.WithContext(ctx).Preload("Edge").Preload("Edge.FromNode").Preload("Edge.ToNode").First(&item, id).Error; err != nil {
		return nil, fmt.Errorf("find ventilation stoppage: %w", err)
	}
	return &item, nil
}

// FindActiveByEdge 返回某条巷道当前未结束的停风；没有时返回 gorm.ErrRecordNotFound。
func (r *VentilationStoppageRepository) FindActiveByEdge(ctx context.Context, edgeID uint) (*model.VentilationStoppage, error) {
	var item model.VentilationStoppage
	if err := r.db.WithContext(ctx).Where("edge_id = ? AND status = ?", edgeID, "active").First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VentilationStoppageRepository) AllActive(ctx context.Context) ([]model.VentilationStoppage, error) {
	var items []model.VentilationStoppage
	if err := r.db.WithContext(ctx).Where("status = ?", "active").Preload("Edge").Order("id ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list active ventilation stoppages: %w", err)
	}
	return items, nil
}

func (r *VentilationStoppageRepository) Create(ctx context.Context, item *model.VentilationStoppage, audit AuditRecord) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(item).Error; err != nil {
			return fmt.Errorf("create ventilation stoppage: %w", err)
		}
		after, _ := json.Marshal(item)
		audit.EntityID = item.ID
		audit.AfterState = string(after)
		return writeAudit(tx, audit)
	})
}

// Recover 用条件更新保证幂等：只有仍处于 active 的停风可以恢复，恢复后巷道推演
// 自动回到登记前风门状态（door_state 从未被改写）。
func (r *VentilationStoppageRepository) Recover(ctx context.Context, id, actorID uint, audit AuditRecord) (*model.VentilationStoppage, error) {
	var updated model.VentilationStoppage
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before model.VentilationStoppage
		if err := tx.Preload("Edge").First(&before, id).Error; err != nil {
			return fmt.Errorf("load ventilation stoppage before recovery: %w", err)
		}
		if before.Status != "active" {
			return gorm.ErrDuplicatedKey
		}
		now := time.Now().UTC()
		result := tx.Model(&model.VentilationStoppage{}).
			Where("id = ? AND status = ?", id, "active").
			Updates(map[string]interface{}{
				"status": "recovered", "actual_restore_at": now,
				"recovered_by": actorID, "active_edge_id": nil,
			})
		if result.Error != nil {
			return fmt.Errorf("recover ventilation stoppage: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return gorm.ErrDuplicatedKey
		}
		if err := tx.Preload("Edge").First(&updated, id).Error; err != nil {
			return err
		}
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(updated)
		audit.EntityID = id
		audit.BeforeState = string(beforeJSON)
		audit.AfterState = string(afterJSON)
		return writeAudit(tx, audit)
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}
