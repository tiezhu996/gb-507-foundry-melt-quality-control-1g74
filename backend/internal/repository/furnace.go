package repository

import (
	"context"
	"errors"
	"time"

	"github.com/blueship581/foundry-melt-quality-control/backend/internal/dto"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/model"
	"gorm.io/gorm"
)

// FurnaceRepository owns all persistence operations for 炉台.
type FurnaceRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.Furnace], error)
	ListAll(context.Context) ([]model.Furnace, error)
	Get(context.Context, uint) (model.Furnace, error)
	GetByCode(context.Context, string) (model.Furnace, error)
	Create(context.Context, *model.Furnace) error
	Update(context.Context, uint, uint, *model.Furnace) error
	// ClaimAvailable atomically reserves an available furnace for charging.
	// The conditional update fails when another request changed the version or
	// moved the furnace out of the available state.
	ClaimAvailable(context.Context, uint, uint) (model.Furnace, error)
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
}

// ErrFurnaceNotAvailable marks a claim attempt whose furnace is no longer available.
var ErrFurnaceNotAvailable = errors.New("furnace is not available for charging")

type furnaceRepository struct {
	store *Store[model.Furnace]
}

func NewFurnaceRepository(db *gorm.DB) FurnaceRepository {
	return &furnaceRepository{store: NewStore[model.Furnace](db)}
}

func (r *furnaceRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.Furnace], error) {
	return r.store.List(ctx, q)
}
func (r *furnaceRepository) ListAll(ctx context.Context) ([]model.Furnace, error) {
	items := make([]model.Furnace, 0)
	err := dbForContext(ctx, r.store.db).Order("code ASC").Find(&items).Error
	return items, err
}
func (r *furnaceRepository) Get(ctx context.Context, id uint) (model.Furnace, error) {
	return r.store.Get(ctx, id)
}
func (r *furnaceRepository) GetByCode(ctx context.Context, code string) (model.Furnace, error) {
	var item model.Furnace
	err := dbForContext(ctx, r.store.db).Where("code = ?", code).First(&item).Error
	return item, err
}
func (r *furnaceRepository) Create(ctx context.Context, item *model.Furnace) error {
	return r.store.Create(ctx, item)
}
func (r *furnaceRepository) Update(ctx context.Context, id, version uint, item *model.Furnace) error {
	return r.store.Update(ctx, id, version, item)
}
func (r *furnaceRepository) ClaimAvailable(ctx context.Context, id, expectedVersion uint) (model.Furnace, error) {
	db := dbForContext(ctx, r.store.db)
	result := db.Model(&model.Furnace{}).
		Where("id = ? AND version = ? AND status = ?", id, expectedVersion, "available").
		Updates(map[string]any{"status": "charging", "version": expectedVersion + 1, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return model.Furnace{}, asVersionConflict(result.Error)
	}
	if result.RowsAffected == 0 {
		var current model.Furnace
		if err := db.First(&current, id).Error; err != nil {
			return model.Furnace{}, err
		}
		return current, ErrFurnaceNotAvailable
	}
	var claimed model.Furnace
	err := db.First(&claimed, id).Error
	return claimed, err
}
func (r *furnaceRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *furnaceRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
