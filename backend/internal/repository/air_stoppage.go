package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"mine-ventilation-network-simulator/backend/internal/constants"
	"mine-ventilation-network-simulator/backend/internal/model"
)

type AirStoppageRepository struct{ db *gorm.DB }

func NewAirStoppageRepository(db *gorm.DB) *AirStoppageRepository {
	return &AirStoppageRepository{db: db}
}

func (r *AirStoppageRepository) List(ctx context.Context, page, pageSize int, status string) ([]model.AirStoppage, int64, error) {
	query := r.db.WithContext(ctx).Model(&model.AirStoppage{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count air stoppages: %w", err)
	}
	var items []model.AirStoppage
	err := query.Preload("Edge").Preload("Edge.FromNode").Preload("Edge.ToNode").
		Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list air stoppages: %w", err)
	}
	return items, total, nil
}

func (r *AirStoppageRepository) Find(ctx context.Context, id uint) (*model.AirStoppage, error) {
	var item model.AirStoppage
	if err := r.db.WithContext(ctx).Preload("Edge").Preload("Edge.FromNode").Preload("Edge.ToNode").First(&item, id).Error; err != nil {
		return nil, fmt.Errorf("find air stoppage: %w", err)
	}
	return &item, nil
}

func (r *AirStoppageRepository) ActiveEdgeIDs(ctx context.Context) ([]uint, error) {
	var ids []uint
	err := r.db.WithContext(ctx).Model(&model.AirStoppage{}).
		Where("status = ?", string(constants.StoppageStatusStopping)).Order("edge_id ASC").Pluck("edge_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("list active stoppage edge ids: %w", err)
	}
	return ids, nil
}

func (r *AirStoppageRepository) HasActiveForEdge(ctx context.Context, edgeID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AirStoppage{}).
		Where("edge_id = ? AND status = ?", edgeID, string(constants.StoppageStatusStopping)).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("count active stoppages for edge: %w", err)
	}
	return count > 0, nil
}

func (r *AirStoppageRepository) Create(ctx context.Context, stoppage *model.AirStoppage, audit AuditRecord) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(stoppage).Error; err != nil {
			return fmt.Errorf("create air stoppage: %w", err)
		}
		after, _ := json.Marshal(stoppage)
		audit.EntityID = stoppage.ID
		audit.AfterState = string(after)
		return writeAudit(tx, audit)
	})
}

func (r *AirStoppageRepository) Restore(ctx context.Context, id, actorID uint, restoredAt time.Time, audit AuditRecord) (*model.AirStoppage, error) {
	var updated model.AirStoppage
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before model.AirStoppage
		if err := tx.First(&before, id).Error; err != nil {
			return fmt.Errorf("load air stoppage before restore: %w", err)
		}
		result := tx.Model(&model.AirStoppage{}).
			Where("id = ? AND status = ?", id, string(constants.StoppageStatusStopping)).
			Updates(map[string]interface{}{
				"status":      string(constants.StoppageStatusRestored),
				"restored_by": actorID,
				"restored_at": restoredAt,
			})
		if result.Error != nil {
			return fmt.Errorf("restore air stoppage: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrVersionConflict
		}
		if err := tx.Preload("Edge").Preload("Edge.FromNode").Preload("Edge.ToNode").First(&updated, id).Error; err != nil {
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
