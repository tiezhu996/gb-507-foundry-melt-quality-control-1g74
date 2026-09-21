package repository

import (
	"context"

	"github.com/blueship581/foundry-melt-quality-control/backend/internal/dto"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/model"
	"gorm.io/gorm"
)

// FurnaceRepository owns all persistence operations for 炉台.
type FurnaceRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.Furnace], error)
	Get(context.Context, uint) (model.Furnace, error)
	GetByCode(context.Context, string) (model.Furnace, error)
	// ListEligibleForRemelt returns currently available furnaces whose declared
	// capability covers the grade, charge weight and target temperature of a
	// heat awaiting remelt. Eligibility is computed inside the repository so the
	// capability predicate is not duplicated across callers.
	ListEligibleForRemelt(ctx context.Context, alloy string, chargeWeightKg, targetTemperatureC float64) ([]model.Furnace, error)
	Create(context.Context, *model.Furnace) error
	Update(context.Context, uint, uint, *model.Furnace) error
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
}

type furnaceRepository struct {
	store *Store[model.Furnace]
}

func NewFurnaceRepository(db *gorm.DB) FurnaceRepository {
	return &furnaceRepository{store: NewStore[model.Furnace](db)}
}

func (r *furnaceRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.Furnace], error) {
	return r.store.List(ctx, q)
}
func (r *furnaceRepository) Get(ctx context.Context, id uint) (model.Furnace, error) {
	return r.store.Get(ctx, id)
}
func (r *furnaceRepository) GetByCode(ctx context.Context, code string) (model.Furnace, error) {
	var item model.Furnace
	err := dbForContext(ctx, r.store.db).Where("code = ?", code).First(&item).Error
	return item, err
}

// ListEligibleForRemelt mirrors the capability checks used when a heat is first
// charged: only an available (ready) furnace can accept the charge, capacity
// must cover the weight, the temperature ceiling the target, and the alloy must
// be among the supported grades. Charging/maintenance/locked furnaces are
// excluded so the closed loop never overbooks a busy furnace.
func (r *furnaceRepository) ListEligibleForRemelt(ctx context.Context, alloy string, chargeWeightKg, targetTemperatureC float64) ([]model.Furnace, error) {
	var items []model.Furnace
	err := dbForContext(ctx, r.store.db).
		Where("status = ?", "available").
		Where("capacity_tonnes * 1000 >= ?", chargeWeightKg).
		Where("max_temperature_c >= ?", targetTemperatureC).
		Order("code ASC").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	eligible := make([]model.Furnace, 0, len(items))
	for _, furnace := range items {
		if alloySupportedAlloy(furnace.SupportedAlloys, alloy) {
			eligible = append(eligible, furnace)
		}
	}
	return eligible, nil
}

func (r *furnaceRepository) Create(ctx context.Context, item *model.Furnace) error {
	return r.store.Create(ctx, item)
}
func (r *furnaceRepository) Update(ctx context.Context, id, version uint, item *model.Furnace) error {
	return r.store.Update(ctx, id, version, item)
}
func (r *furnaceRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *furnaceRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
