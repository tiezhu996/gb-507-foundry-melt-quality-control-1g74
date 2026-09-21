package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blueship581/foundry-melt-quality-control/backend/internal/config"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/dto"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/model"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/repository"
	"gorm.io/gorm"
)

func mustCreate(t *testing.T, db *gorm.DB, values ...any) {
	t.Helper()
	for _, value := range values {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("seed %T: %v", value, err)
		}
	}
}

func remeltDecision(t *testing.T, db *gorm.DB, code, heatCode, sampleCode string) model.QualityDecision {
	t.Helper()
	decision := model.QualityDecision{
		BaseModel: model.BaseModel{Code: code, Name: "Remelt decision", Status: "draft", Version: 1},
		HeatCode:  heatCode, SampleCode: sampleCode, Reviewer: "reviewer", Reason: "成分超限需要返炉",
		DecidedAt: time.Now().UTC().Add(-time.Minute), Evidence: "QMS-" + code,
	}
	if err := db.Create(&decision).Error; err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	return decision
}

func TestRemeltHandoverClosesTheLoopAtomically(t *testing.T) {
	db := workflowTestDB(t)
	ctx := context.Background()
	furnaceRepository := repository.NewFurnaceRepository(db)
	heatRepository := repository.NewHeatRepository(db)
	sampleRepository := repository.NewChemicalSampleRepository(db)
	decisionRepository := repository.NewQualityDecisionRepository(db)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{AppName: "test", JWTSecret: strings.Repeat("a", 32)})
	decisions := NewQualityDecisionService(decisionRepository, heatRepository, furnaceRepository, sampleRepository, security)

	furnace := remeltFurnace("F-RM-01", "available")
	heat := workflowHeat("H-RM-01")
	sample := workflowSample("S-RM-01", heat.Code)
	mustCreate(t, db, &furnace, &heat, &sample)
	decision := remeltDecision(t, db, "QD-RM-01", heat.Code, sample.Code)

	result, err := decisions.RemeltHandover(ctx, decision.ID, dto.RemeltHandover{
		ExpectedVersion: 1, FurnaceCode: "F-RM-01", ReturnHeatCode: "H-RM-RET-01",
		ReturnHeatName: "返炉承接炉次", Reason: "碳当量超标返炉重熔", Evidence: "QMS-RET-01",
	}, "reviewer", "req-remelt")
	if err != nil {
		t.Fatalf("remelt handover: %v", err)
	}

	if result.Decision.Status != "remelt" || result.Decision.RemeltFurnaceCode != "F-RM-01" || result.Decision.RemeltHeatCode != "H-RM-RET-01" {
		t.Fatalf("decision links not persisted: %#v", result.Decision)
	}
	if result.OriginHeat.Status != "rejected" || result.OriginHeat.ReturnedHeatCode != "H-RM-RET-01" {
		t.Fatalf("original heat not rejected and linked: %#v", result.OriginHeat)
	}
	if result.ReturnHeat.Status != "charged" || result.ReturnHeat.OriginHeatCode != "H-RM-01" ||
		result.ReturnHeat.FurnaceCode != "F-RM-01" {
		t.Fatalf("return heat not created as charged on承接 furnace: %#v", result.ReturnHeat)
	}
	if result.Furnace.Status != "charging" {
		t.Fatalf("furnace was not reserved: %#v", result.Furnace)
	}

	// The return heat inherits the frozen chemistry specification and process constraints.
	origin, _ := heatRepository.GetByCode(ctx, "H-RM-01")
	returned, err := heatRepository.GetByCode(ctx, "H-RM-RET-01")
	if err != nil {
		t.Fatalf("read return heat: %v", err)
	}
	if returned.AlloyGrade != origin.AlloyGrade || returned.ChargeWeightKg != origin.ChargeWeightKg ||
		returned.TargetTemperatureC != origin.TargetTemperatureC ||
		returned.CarbonMinPct != origin.CarbonMinPct || returned.CarbonMaxPct != origin.CarbonMaxPct ||
		returned.SiliconMinPct != origin.SiliconMinPct || returned.SiliconMaxPct != origin.SiliconMaxPct ||
		returned.SulfurMaxPct != origin.SulfurMaxPct || returned.PhosphorusMaxPct != origin.PhosphorusMaxPct {
		t.Fatalf("return heat did not inherit the frozen specification: origin=%#v returned=%#v", origin, returned)
	}

	storedFurnace, _ := furnaceRepository.GetByCode(ctx, "F-RM-01")
	if storedFurnace.Status != "charging" || storedFurnace.Version != 2 {
		t.Fatalf("furnace state not persisted atomically: %#v", storedFurnace)
	}
	storedOrigin, _ := heatRepository.GetByCode(ctx, "H-RM-01")
	if storedOrigin.Status != "rejected" || storedOrigin.Version != 2 {
		t.Fatalf("original heat state not persisted: %#v", storedOrigin)
	}

	var auditCount int64
	if err := db.Model(&model.AuditLog{}).Where("request_id = ?", "req-remelt").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if auditCount != 4 {
		t.Fatalf("expected furnace, return heat, origin heat and decision audits, got %d", auditCount)
	}

	// The new heat starts its ordinary life at charged and can advance from charging state onward.
	if _, err := NewHeatService(heatRepository, furnaceRepository, sampleRepository, security).Transition(ctx, returned.ID, dto.TransitionRequest{
		Status: "melting", ExpectedVersion: 1, Reason: "return heat starts melting",
	}, "operator", "req-melt-on"); err != nil {
		t.Fatalf("return heat should advance from charged like a normal heat: %v", err)
	}
}

func TestRemeltRejectsNonCompliantFurnacesWithoutWrites(t *testing.T) {
	cases := []struct {
		name    string
		furnace model.Furnace
		want    string
	}{
		{"maintenance", remeltFurnace("F-BAD-MAINT", "maintenance"), "not available"},
		{"charging", remeltFurnace("F-BAD-BUSY", "charging"), "not available"},
		{"capacity", remeltFurnaceWith("F-BAD-CAP", "available", 0.5, 1600, "HT250"), "capacity"},
		{"temperature", remeltFurnaceWith("F-BAD-TEMP", "available", 12, 1400, "HT250"), "temperature"},
		{"alloy", remeltFurnaceWith("F-BAD-ALLOY", "available", 12, 1600, "QT450-10"), "alloy grade"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := workflowTestDB(t)
			ctx := context.Background()
			furnaceRepository := repository.NewFurnaceRepository(db)
			heatRepository := repository.NewHeatRepository(db)
			sampleRepository := repository.NewChemicalSampleRepository(db)
			decisionRepository := repository.NewQualityDecisionRepository(db)
			security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{AppName: "test", JWTSecret: strings.Repeat("a", 32)})
			decisions := NewQualityDecisionService(decisionRepository, heatRepository, furnaceRepository, sampleRepository, security)

			heat := workflowHeat("H-RM-BAD")
			sample := workflowSample("S-RM-BAD", heat.Code)
			mustCreate(t, db, &tc.furnace, &heat, &sample)
			decision := remeltDecision(t, db, "QD-RM-BAD", heat.Code, sample.Code)

			_, err := decisions.RemeltHandover(ctx, decision.ID, dto.RemeltHandover{
				ExpectedVersion: 1, FurnaceCode: tc.furnace.Code, ReturnHeatCode: "H-RM-BAD-RET",
				ReturnHeatName: "返炉炉次", Reason: "碳当量超标返炉重熔", Evidence: "QMS-RET-BAD",
			}, "reviewer", "req-bad")
			if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected invalid input mentioning %q, got %v", tc.want, err)
			}
			assertNoRemeltSideEffects(t, db, decision.ID, tc.furnace.Code, "H-RM-BAD")
		})
	}
}

func TestRemeltRejectsDuplicateAndConcurrentSubmissions(t *testing.T) {
	db := workflowTestDB(t)
	ctx := context.Background()
	furnaceRepository := repository.NewFurnaceRepository(db)
	heatRepository := repository.NewHeatRepository(db)
	sampleRepository := repository.NewChemicalSampleRepository(db)
	decisionRepository := repository.NewQualityDecisionRepository(db)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{AppName: "test", JWTSecret: strings.Repeat("a", 32)})
	decisions := NewQualityDecisionService(decisionRepository, heatRepository, furnaceRepository, sampleRepository, security)

	furnace := remeltFurnace("F-RM-DUP", "available")
	heat := workflowHeat("H-RM-DUP")
	sample := workflowSample("S-RM-DUP", heat.Code)
	mustCreate(t, db, &furnace, &heat, &sample)
	decision := remeltDecision(t, db, "QD-RM-DUP", heat.Code, sample.Code)
	submit := func(version uint, code, furnaceCode string) error {
		_, err := decisions.RemeltHandover(ctx, decision.ID, dto.RemeltHandover{
			ExpectedVersion: version, FurnaceCode: furnaceCode, ReturnHeatCode: code,
			ReturnHeatName: "返炉炉次", Reason: "碳当量超标返炉重熔", Evidence: "QMS-" + code,
		}, "reviewer", "req-dup")
		return err
	}

	if err := submit(1, "H-RM-DUP-RET", "F-RM-DUP"); err != nil {
		t.Fatalf("first submission: %v", err)
	}
	// Duplicate replay of the completed loop must be rejected.
	if err := submit(1, "H-RM-DUP-RET", "F-RM-DUP"); !errors.Is(err, ErrInvalidTransition) && !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("expected duplicate submission to fail with state/version error, got %v", err)
	}
	// A stale optimistic-lock version cannot produce a second return heat either.
	if err := submit(1, "H-RM-DUP-RET-2", "F-RM-DUP"); err == nil {
		t.Fatal("expected stale version submission to fail")
	}

	var returnCount int64
	if err := db.Model(&model.Heat{}).Where("origin_heat_code = ?", "H-RM-DUP").Count(&returnCount).Error; err != nil {
		t.Fatalf("count return heats: %v", err)
	}
	if returnCount != 1 {
		t.Fatalf("expected exactly one return heat after duplicate attempts, got %d", returnCount)
	}
}

func TestGenericTransitionCannotSignRemelt(t *testing.T) {
	db := workflowTestDB(t)
	ctx := context.Background()
	furnaceRepository := repository.NewFurnaceRepository(db)
	heatRepository := repository.NewHeatRepository(db)
	sampleRepository := repository.NewChemicalSampleRepository(db)
	decisionRepository := repository.NewQualityDecisionRepository(db)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{AppName: "test", JWTSecret: strings.Repeat("a", 32)})
	decisions := NewQualityDecisionService(decisionRepository, heatRepository, furnaceRepository, sampleRepository, security)

	furnace := remeltFurnace("F-RM-GATE", "available")
	heat := workflowHeat("H-RM-GATE")
	sample := workflowSample("S-RM-GATE", heat.Code)
	mustCreate(t, db, &furnace, &heat, &sample)
	decision := remeltDecision(t, db, "QD-RM-GATE", heat.Code, sample.Code)

	if _, err := decisions.Transition(ctx, decision.ID, dto.TransitionRequest{
		Status: "remelt", ExpectedVersion: 1, Reason: "attempt generic remelt",
	}, "reviewer", "req-gate"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected generic transition to refuse remelt, got %v", err)
	}
	assertNoRemeltSideEffects(t, db, decision.ID, "F-RM-GATE", "H-RM-GATE")
}

func TestRemeltFurnaceCandidatesCarryReasons(t *testing.T) {
	db := workflowTestDB(t)
	ctx := context.Background()
	furnaceRepository := repository.NewFurnaceRepository(db)
	heatRepository := repository.NewHeatRepository(db)
	sampleRepository := repository.NewChemicalSampleRepository(db)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{AppName: "test", JWTSecret: strings.Repeat("a", 32)})
	heats := NewHeatService(heatRepository, furnaceRepository, sampleRepository, security)

	available := remeltFurnace("F-CAND-OK", "available")
	busy := remeltFurnace("F-CAND-BUSY", "charging")
	maintenance := remeltFurnaceWith("F-CAND-MAINT", "maintenance", 12, 1600, "HT250")
	mustCreate(t, db, &available, &busy, &maintenance)
	heat := workflowHeat("H-CAND")
	if err := db.Create(&heat).Error; err != nil {
		t.Fatalf("seed heat: %v", err)
	}

	options, err := heats.RemeltFurnaces(ctx, "H-CAND")
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	if len(options) != 3 {
		t.Fatalf("expected all three furnaces evaluated, got %d", len(options))
	}
	byCode := map[string]dto.RemeltFurnaceOption{}
	for _, option := range options {
		byCode[option.Code] = option
	}
	if !byCode["F-CAND-OK"].Eligible || len(byCode["F-CAND-OK"].Reasons) != 0 {
		t.Fatalf("available capable furnace should be eligible: %#v", byCode["F-CAND-OK"])
	}
	if byCode["F-CAND-BUSY"].Eligible || len(byCode["F-CAND-BUSY"].Reasons) == 0 {
		t.Fatalf("charging furnace must be excluded with a reason: %#v", byCode["F-CAND-BUSY"])
	}
	if byCode["F-CAND-MAINT"].Eligible || len(byCode["F-CAND-MAINT"].Reasons) == 0 {
		t.Fatalf("maintenance furnace must be excluded with a reason: %#v", byCode["F-CAND-MAINT"])
	}
}

func assertNoRemeltSideEffects(t *testing.T, db *gorm.DB, decisionID uint, furnaceCode, heatCode string) {
	t.Helper()
	var decision model.QualityDecision
	if err := db.First(&decision, decisionID).Error; err != nil {
		t.Fatalf("reload decision: %v", err)
	}
	if decision.Status != "draft" || decision.RemeltHeatCode != "" {
		t.Fatalf("decision was modified by failed remelt: %#v", decision)
	}
	var furnace model.Furnace
	if err := db.Where("code = ?", furnaceCode).First(&furnace).Error; err != nil {
		t.Fatalf("reload furnace: %v", err)
	}
	if furnace.Version != 1 {
		t.Fatalf("furnace was modified by failed remelt: %#v", furnace)
	}
	var heat model.Heat
	if err := db.Where("code = ?", heatCode).First(&heat).Error; err != nil {
		t.Fatalf("reload heat: %v", err)
	}
	if heat.Status != "hold" || heat.Version != 1 || heat.ReturnedHeatCode != "" {
		t.Fatalf("original heat was modified by failed remelt: %#v", heat)
	}
	var returnCount int64
	if err := db.Model(&model.Heat{}).Where("origin_heat_code = ?", heatCode).Count(&returnCount).Error; err != nil {
		t.Fatalf("count return heats: %v", err)
	}
	if returnCount != 0 {
		t.Fatalf("failed remelt created %d return heats", returnCount)
	}
	var auditCount int64
	if err := db.Model(&model.AuditLog{}).Where("request_id = ?", "req-bad").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if auditCount != 0 {
		t.Fatalf("failed remelt wrote %d audit rows", auditCount)
	}
}

func remeltFurnace(code, status string) model.Furnace {
	return remeltFurnaceWith(code, status, 12, 1600, "HT250")
}

func remeltFurnaceWith(code, status string, capacityTonnes, maxTemperature float64, alloys string) model.Furnace {
	return model.Furnace{
		BaseModel: model.BaseModel{Code: code, Name: code + " 炉台", Status: status, Version: 1},
		PlantArea: "熔炼一车间", FurnaceType: "中频感应炉", CapacityTonnes: capacityTonnes,
		SupportedAlloys: alloys, MaxTemperatureC: maxTemperature, Operator: "熔炼甲班",
		LastInspectionAt: time.Now().UTC().Add(-time.Hour), Evidence: "INS-" + code,
	}
}
