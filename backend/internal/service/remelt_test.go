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

func setupRemelt(t *testing.T) *serviceBundle {
	t.Helper()
	db := workflowTestDB(t)
	ctx := context.Background()

	heatRepository := repository.NewHeatRepository(db)
	furnaceRepository := repository.NewFurnaceRepository(db)
	sampleRepository := repository.NewChemicalSampleRepository(db)
	decisionRepository := repository.NewQualityDecisionRepository(db)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{AppName: "test", JWTSecret: strings.Repeat("a", 32)})
	decisionService := NewQualityDecisionService(decisionRepository, heatRepository, furnaceRepository, sampleRepository, security)
	heatService := NewHeatService(heatRepository, furnaceRepository, sampleRepository, security)
	furnaceService := NewFurnaceService(furnaceRepository, security)

	// Available furnace that matches the HT250 / 1000kg / 1500C seed heat.
	furnace := model.Furnace{
		BaseModel: model.BaseModel{Code: "F-REMELT", Name: "Remelt furnace", Status: "available", Version: 1},
		PlantArea: "A", FurnaceType: "induction", CapacityTonnes: 10, SupportedAlloys: "HT250,QT450-10",
		MaxTemperatureC: 1600, Operator: "operator", LastInspectionAt: time.Now().UTC().Add(-time.Hour),
		Evidence: "inspection-report",
	}
	if err := db.Create(&furnace).Error; err != nil {
		t.Fatalf("seed furnace: %v", err)
	}

	heat := workflowHeat("H-REMELT-01")
	if err := db.Create(&heat).Error; err != nil {
		t.Fatalf("seed heat: %v", err)
	}
	sample := workflowSample("S-REMELT-01", heat.Code)
	if err := db.Create(&sample).Error; err != nil {
		t.Fatalf("seed sample: %v", err)
	}

	created, err := decisionService.Create(ctx, dto.CreateQualityDecision{
		Code: "QD-REMELT-01", Name: "Remelt decision", HeatCode: heat.Code, SampleCode: sample.Code,
		Reviewer: "reviewer", Reason: "chemistry off target", DecidedAt: time.Now().UTC(), Evidence: "LIMS-remelt",
	}, "reviewer", "req-create")
	if err != nil {
		t.Fatalf("create draft decision: %v", err)
	}

	return &serviceBundle{
		db: db, ctx: ctx, heatRepo: heatRepository, furnaceRepo: furnaceRepository, sampleRepo: sampleRepository,
		decisionRepo: decisionRepository, decisions: decisionService, heats: heatService, furnaces: furnaceService,
		origin: heat, sample: sample, decision: created,
	}
}

type serviceBundle struct {
	db           *gorm.DB
	ctx          context.Context
	heatRepo     repository.HeatRepository
	furnaceRepo  repository.FurnaceRepository
	sampleRepo   repository.ChemicalSampleRepository
	decisionRepo repository.QualityDecisionRepository
	decisions    QualityDecisionService
	heats        HeatService
	furnaces     FurnaceService
	origin       model.Heat
	sample       model.ChemicalSample
	decision     model.QualityDecision
}

func validRemeltRequest(furnaceCode string) dto.RemeltRequest {
	return dto.RemeltRequest{
		ExpectedVersion: 1, Reason: "return to compliant furnace",
		FurnaceCode: furnaceCode, ReturnHeatCode: "H-RETURN-01", ReturnHeatName: "返炉承接炉次",
		Owner: "熔炼乙班", Evidence: "MES-RETURN-01",
	}
}

func TestRemeltClosesLoopAtomicallyAndInheritsSpecification(t *testing.T) {
	bundle := setupRemelt(t)

	finalized, err := bundle.decisions.Remelt(bundle.ctx, bundle.decision.ID, validRemeltRequest("F-REMELT"), "reviewer", "req-remelt")
	if err != nil {
		t.Fatalf("remelt: %v", err)
	}
	if finalized.Status != "remelt" || finalized.RemeltHeatCode != "H-RETURN-01" || finalized.RemeltFurnaceCode != "F-REMELT" || finalized.RemeltedAt == nil {
		t.Fatalf("decision missing remelt linkage: %#v", finalized)
	}

	origin, err := bundle.heatRepo.GetByCode(bundle.ctx, "H-REMELT-01")
	if err != nil {
		t.Fatalf("load origin: %v", err)
	}
	if origin.Status != "rejected" || origin.RemeltedIntoCode != "H-RETURN-01" {
		t.Fatalf("origin not rejected+linked: %#v", origin)
	}

	child, err := bundle.heatRepo.GetByCode(bundle.ctx, "H-RETURN-01")
	if err != nil {
		t.Fatalf("load return heat: %v", err)
	}
	if child.Status != "charged" || child.RemeltOfCode != origin.Code {
		t.Fatalf("return heat not charged and linked: %#v", child)
	}
	// Frozen chemistry and process spec must be inherited verbatim.
	if child.AlloyGrade != origin.AlloyGrade || child.ChargeWeightKg != origin.ChargeWeightKg ||
		child.TargetTemperatureC != origin.TargetTemperatureC ||
		child.CarbonMinPct != origin.CarbonMinPct || child.CarbonMaxPct != origin.CarbonMaxPct ||
		child.SiliconMinPct != origin.SiliconMinPct || child.SiliconMaxPct != origin.SiliconMaxPct ||
		child.SulfurMaxPct != origin.SulfurMaxPct || child.PhosphorusMaxPct != origin.PhosphorusMaxPct {
		t.Fatalf("return heat did not inherit the frozen specification:\norigin=%#v\nchild=%#v", origin, child)
	}
	if child.FurnaceCode != "F-REMELT" || child.Owner != "熔炼乙班" {
		t.Fatalf("return heat furnace/owner mismatch: %#v", child)
	}

	furnace, err := bundle.furnaceRepo.GetByCode(bundle.ctx, "F-REMELT")
	if err != nil {
		t.Fatalf("load furnace: %v", err)
	}
	if furnace.Status != "charging" {
		t.Fatalf("furnace not reserved, status=%s", furnace.Status)
	}

	// Four audit rows: decision remelt, origin rejection, return heat create,
	// furnace reservation.
	var auditCount int64
	if err := bundle.db.Model(&model.AuditLog{}).Where("request_id = ?", "req-remelt").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if auditCount != 4 {
		t.Fatalf("expected 4 closed-loop audits, got %d", auditCount)
	}
}

func TestRemeltRejectsWhenNoCompliantFurnace(t *testing.T) {
	bundle := setupRemelt(t)

	// A furnace that supports the alloy but has insufficient capacity.
	small := model.Furnace{
		BaseModel: model.BaseModel{Code: "F-SMALL", Name: "Small furnace", Status: "available", Version: 1},
		PlantArea: "A", FurnaceType: "induction", CapacityTonnes: 0.5, SupportedAlloys: "HT250",
		MaxTemperatureC: 1600, Operator: "op", LastInspectionAt: time.Now().UTC().Add(-time.Hour), Evidence: "insp",
	}
	if err := bundle.db.Create(&small).Error; err != nil {
		t.Fatalf("seed small furnace: %v", err)
	}

	request := validRemeltRequest("F-SMALL")
	request.ReturnHeatCode = "H-RETURN-SMALL"
	_, err := bundle.decisions.Remelt(bundle.ctx, bundle.decision.ID, request, "reviewer", "req-small")
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("expected capacity business rule, got %v", err)
	}
	assertRemeltUntouched(t, bundle)

	// Eligible preview must list only the compliant furnace (not F-SMALL).
	options, err := bundle.decisions.EligibleFurnaces(bundle.ctx, bundle.decision.ID)
	if err != nil {
		t.Fatalf("eligible furnaces: %v", err)
	}
	if len(options) != 1 || options[0].Code != "F-REMELT" {
		t.Fatalf("expected only F-REMELT eligible, got %#v", options)
	}

	// When no compliant furnace exists at all, preview is empty and remelt on a
	// maintenance furnace names that reason.
	if _, err := bundle.furnaces.Transition(bundle.ctx, mustFurnaceID(t, bundle, "F-REMELT"), dto.TransitionRequest{
		Status: "maintenance", ExpectedVersion: 1, Reason: "unplanned repair",
	}, "operator", "req-maint"); err != nil {
		t.Fatalf("move furnace to maintenance: %v", err)
	}
	options, err = bundle.decisions.EligibleFurnaces(bundle.ctx, bundle.decision.ID)
	if err != nil {
		t.Fatalf("eligible furnaces after maintenance: %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("expected no eligible furnaces, got %#v", options)
	}
	request = validRemeltRequest("F-REMELT")
	if _, err := bundle.decisions.Remelt(bundle.ctx, bundle.decision.ID, request, "reviewer", "req-maint-remelt"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected unavailable furnace rejection, got %v", err)
	}
	assertRemeltUntouched(t, bundle, "maintenance")
}

func TestRemeltRejectsNonDraftAndDuplicateSubmission(t *testing.T) {
	bundle := setupRemelt(t)

	// First submission succeeds.
	if _, err := bundle.decisions.Remelt(bundle.ctx, bundle.decision.ID, validRemeltRequest("F-REMELT"), "reviewer", "req-first"); err != nil {
		t.Fatalf("first remelt: %v", err)
	}

	// Replaying the same closed loop (same return heat code) must fail without
	// creating a second child or mutating anything.
	_, err := bundle.decisions.Remelt(bundle.ctx, bundle.decision.ID, validRemeltRequest("F-REMELT"), "reviewer", "req-second")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected non-draft rejection, got %v", err)
	}

	// Only one return heat exists for the origin.
	hasChild, err := bundle.heatRepo.HasRemeltChild(bundle.ctx, "H-REMELT-01")
	if err != nil {
		t.Fatalf("check children: %v", err)
	}
	if !hasChild {
		t.Fatal("return child disappeared")
	}
	var childCount int64
	if err := bundle.db.Model(&model.Heat{}).Where("remelt_of_code = ?", "H-REMELT-01").Count(&childCount).Error; err != nil {
		t.Fatalf("count children: %v", err)
	}
	if childCount != 1 {
		t.Fatalf("expected exactly one return heat, got %d", childCount)
	}
}

func TestRemeltRejectsStaleDecisionVersion(t *testing.T) {
	bundle := setupRemelt(t)

	request := validRemeltRequest("F-REMELT")
	request.ExpectedVersion = 99 // stale
	_, err := bundle.decisions.Remelt(bundle.ctx, bundle.decision.ID, request, "reviewer", "req-stale-decision")
	if !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("expected decision version conflict, got %v", err)
	}
	assertRemeltUntouched(t, bundle)
}

func TestRemeltRejectsWhenFurnaceTakenConcurrently(t *testing.T) {
	bundle := setupRemelt(t)

	// Simulate a concurrent request that reserved the only compliant furnace
	// (available -> charging) between preview and submit.
	if _, err := bundle.furnaces.Transition(bundle.ctx, mustFurnaceID(t, bundle, "F-REMELT"), dto.TransitionRequest{
		Status: "charging", ExpectedVersion: 1, Reason: "concurrent charge",
	}, "operator", "req-concurrent-furnace"); err != nil {
		t.Fatalf("reserve furnace concurrently: %v", err)
	}

	_, err := bundle.decisions.Remelt(bundle.ctx, bundle.decision.ID, validRemeltRequest("F-REMELT"), "reviewer", "req-loser")
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "not currently available") {
		t.Fatalf("expected furnace-unavailable rejection, got %v", err)
	}
	assertRemeltUntouched(t, bundle, "charging")
}

func TestRemeltRejectsDuplicateReturnHeatCode(t *testing.T) {
	bundle := setupRemelt(t)

	// Pre-create a heat with the same code the request wants to use.
	existing := workflowHeat("H-RETURN-01")
	existing.FurnaceCode = "F-REMELT"
	if err := bundle.db.Create(&existing).Error; err != nil {
		t.Fatalf("seed existing return code: %v", err)
	}
	_, err := bundle.decisions.Remelt(bundle.ctx, bundle.decision.ID, validRemeltRequest("F-REMELT"), "reviewer", "req-dup-code")
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate code rejection, got %v", err)
	}
	assertRemeltUntouched(t, bundle)
}

func TestGenericTransitionCannotReachRemelt(t *testing.T) {
	bundle := setupRemelt(t)

	_, err := bundle.decisions.Transition(bundle.ctx, bundle.decision.ID, dto.TransitionRequest{
		Status: "remelt", ExpectedVersion: 1, Reason: "attempt bypass",
	}, "reviewer", "req-bypass-remelt")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected generic transition to refuse remelt, got %v", err)
	}
	assertRemeltUntouched(t, bundle)
}

func TestAcceptAndScrapFlowsRemainUnchanged(t *testing.T) {
	bundle := setupRemelt(t)

	// Accept: the seed sample is within spec (HT250 ranges from workflowHeat and
	// workflowSample), so acceptance still drives the heat to accepted and
	// creates no remelt linkage.
	accepted, err := bundle.decisions.Transition(bundle.ctx, bundle.decision.ID, dto.TransitionRequest{
		Status: "accept", ExpectedVersion: 1, Reason: "within spec",
	}, "reviewer", "req-accept")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if accepted.Status != "accept" || accepted.RemeltHeatCode != "" {
		t.Fatalf("accept must not create remelt linkage: %#v", accepted)
	}
	origin, err := bundle.heatRepo.GetByCode(bundle.ctx, "H-REMELT-01")
	if err != nil {
		t.Fatalf("load origin: %v", err)
	}
	if origin.Status != "accepted" || origin.RemeltedIntoCode != "" {
		t.Fatalf("accepted heat must not carry remelt linkage: %#v", origin)
	}
}

func TestRemeltRejectsWhenHeatLeftHold(t *testing.T) {
	bundle := setupRemelt(t)

	// Move the origin heat away from hold (simulate an out-of-band state change).
	origin := bundle.origin
	origin.Status = "sampling"
	origin.Version = 2
	if err := bundle.heatRepo.Update(bundle.ctx, origin.ID, 1, &origin); err != nil {
		t.Fatalf("move heat off hold: %v", err)
	}
	_, err := bundle.decisions.Remelt(bundle.ctx, bundle.decision.ID, validRemeltRequest("F-REMELT"), "reviewer", "req-not-hold")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected heat-not-on-hold rejection, got %v", err)
	}
}

// assertRemeltUntouched verifies a failed remelt changed no aggregate: the
// decision stays draft, the origin stays hold, no child heat exists and the
// receiving furnace retains its pre-submission status (available, unless the
// test intentionally moved it to simulate concurrency).
func assertRemeltUntouched(t *testing.T, bundle *serviceBundle, expectedFurnaceStatus ...string) {
	t.Helper()
	expectedStatus := "available"
	if len(expectedFurnaceStatus) > 0 {
		expectedStatus = expectedFurnaceStatus[0]
	}
	decision, err := bundle.decisionRepo.Get(bundle.ctx, bundle.decision.ID)
	if err != nil {
		t.Fatalf("reload decision: %v", err)
	}
	if decision.Status != "draft" || decision.RemeltHeatCode != "" || decision.Version != 1 {
		t.Fatalf("decision was mutated by failed remelt: %#v", decision)
	}
	origin, err := bundle.heatRepo.GetByCode(bundle.ctx, "H-REMELT-01")
	if err != nil {
		t.Fatalf("reload origin: %v", err)
	}
	if origin.Status != "hold" || origin.RemeltedIntoCode != "" || origin.Version != 1 {
		t.Fatalf("origin heat was mutated by failed remelt: %#v", origin)
	}
	var childCount int64
	if err := bundle.db.Model(&model.Heat{}).Where("remelt_of_code = ?", "H-REMELT-01").Count(&childCount).Error; err != nil {
		t.Fatalf("count child heats: %v", err)
	}
	if childCount != 0 {
		t.Fatalf("failed remelt created %d child heats", childCount)
	}
	furnace, err := bundle.furnaceRepo.GetByCode(bundle.ctx, "F-REMELT")
	if err != nil {
		t.Fatalf("reload furnace: %v", err)
	}
	if furnace.Status != expectedStatus {
		t.Fatalf("furnace status changed unexpectedly by failed remelt: got=%s want=%s", furnace.Status, expectedStatus)
	}
}

func mustFurnaceID(t *testing.T, bundle *serviceBundle, code string) uint {
	t.Helper()
	furnace, err := bundle.furnaceRepo.GetByCode(bundle.ctx, code)
	if err != nil {
		t.Fatalf("load furnace %s: %v", code, err)
	}
	return furnace.ID
}
