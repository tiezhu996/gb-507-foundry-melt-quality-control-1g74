package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blueship581/foundry-melt-quality-control/backend/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func claimTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Furnace{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestClaimAvailableIsConditional(t *testing.T) {
	db := claimTestDB(t)
	ctx := context.Background()
	repo := NewFurnaceRepository(db)
	furnace := model.Furnace{
		BaseModel: model.BaseModel{Code: "F-CLAIM", Name: "Claim furnace", Status: "available", Version: 1},
		PlantArea: "A", FurnaceType: "induction", CapacityTonnes: 12, SupportedAlloys: "HT250",
		MaxTemperatureC: 1600, Operator: "operator", LastInspectionAt: time.Now().UTC().Add(-time.Hour), Evidence: "INS",
	}
	if err := db.Create(&furnace).Error; err != nil {
		t.Fatalf("seed furnace: %v", err)
	}

	claimed, err := repo.ClaimAvailable(ctx, furnace.ID, 1)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if claimed.Status != "charging" || claimed.Version != 2 {
		t.Fatalf("unexpected claimed furnace: %#v", claimed)
	}

	// A concurrent claimer holding the stale version must lose; ClaimAvailable
	// returns the current furnace so callers can report the concrete reason.
	stale, err := repo.ClaimAvailable(ctx, furnace.ID, 1)
	if !errors.Is(err, ErrFurnaceNotAvailable) {
		t.Fatalf("expected concurrent claim to fail with ErrFurnaceNotAvailable, got %v", err)
	}
	if stale.Status != "charging" || stale.Version != 2 {
		t.Fatalf("failed claim should return current furnace state, got %#v", stale)
	}

	// Even a fresh version cannot claim a furnace that has left the available state.
	if _, err := repo.ClaimAvailable(ctx, furnace.ID, 2); !errors.Is(err, ErrFurnaceNotAvailable) {
		t.Fatalf("claiming a charging furnace must fail, got %v", err)
	}
}
