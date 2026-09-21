package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/foundry-melt-quality-control/backend/internal/constants"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/dto"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/model"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/repository"
)

type QualityDecisionService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.QualityDecision], error)
	Get(context.Context, uint) (model.QualityDecision, error)
	Create(context.Context, dto.CreateQualityDecision, string, string) (model.QualityDecision, error)
	Update(context.Context, uint, dto.UpdateQualityDecision, string, string) (model.QualityDecision, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string) (model.QualityDecision, error)
	// EligibleFurnaces returns the furnaces currently able to receive the heat
	// referenced by a draft remelt decision. It powers the operator's picker and
	// re-runs the same capability rules inside Remelt.
	EligibleFurnaces(ctx context.Context, decisionID uint) ([]dto.RemeltFurnaceOption, error)
	// Remelt closes the "判定返炉" loop atomically: it signs the decision as
	// remelt, rejects the original heat, opens a linked return heat on a
	// compliant furnace (inheriting the frozen chemistry), and reserves that
	// furnace. Any rule failure rolls back every write.
	Remelt(ctx context.Context, decisionID uint, input dto.RemeltRequest, actor, requestID string) (model.QualityDecision, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type qualityDecisionService struct {
	repository repository.QualityDecisionRepository
	heats      repository.HeatRepository
	furnaces   repository.FurnaceRepository
	samples    repository.ChemicalSampleRepository
	security   SecurityService
}

func NewQualityDecisionService(repo repository.QualityDecisionRepository, heats repository.HeatRepository,
	furnaces repository.FurnaceRepository, samples repository.ChemicalSampleRepository, security SecurityService) QualityDecisionService {
	return &qualityDecisionService{repository: repo, heats: heats, furnaces: furnaces, samples: samples, security: security}
}

func (s *qualityDecisionService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.QualityDecision], error) {
	return s.repository.List(ctx, query)
}

func (s *qualityDecisionService) Get(ctx context.Context, id uint) (model.QualityDecision, error) {
	return s.repository.Get(ctx, id)
}

func (s *qualityDecisionService) Create(ctx context.Context, input dto.CreateQualityDecision, actor, requestID string) (model.QualityDecision, error) {
	return atomicValue(ctx, s.security, func(txCtx context.Context) (model.QualityDecision, error) {
		if err := validateQualityDecision(input.Code, input.Name, input.HeatCode, input.SampleCode, actor,
			input.Reason, input.DecidedAt, input.Evidence); err != nil {
			return model.QualityDecision{}, err
		}
		heat, sample, err := s.resolveContext(txCtx, input.HeatCode, input.SampleCode)
		if err != nil {
			return model.QualityDecision{}, err
		}
		if exists, duplicateErr := s.repository.HasForHeat(txCtx, heat.Code, 0); duplicateErr != nil {
			return model.QualityDecision{}, duplicateErr
		} else if exists {
			return model.QualityDecision{}, fmt.Errorf("%w: heat already has a final quality decision", ErrInvalidInput)
		}
		item := model.QualityDecision{
			BaseModel: model.BaseModel{
				Code: normalizeCode(input.Code), Name: strings.TrimSpace(input.Name), Status: model.QualityDecisionInitialStatus,
				Version: 1, Description: strings.TrimSpace(input.Description),
			},
			HeatCode: heat.Code, SampleCode: sample.Code, Reviewer: strings.TrimSpace(actor),
			Reason: strings.TrimSpace(input.Reason), Conditions: strings.TrimSpace(input.Conditions),
			DecidedAt: input.DecidedAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		}
		if err := s.repository.Create(txCtx, &item); err != nil {
			return model.QualityDecision{}, fmt.Errorf("create quality decision: %w", err)
		}
		if err := s.security.Audit(txCtx, actor, requestID, "create", "QualityDecision", item.ID, "", item.Status,
			decisionAuditDetail(item)); err != nil {
			return model.QualityDecision{}, fmt.Errorf("persist decision audit: %w", err)
		}
		return item, nil
	})
}

func (s *qualityDecisionService) Update(ctx context.Context, id uint, input dto.UpdateQualityDecision, actor, requestID string) (model.QualityDecision, error) {
	return atomicValue(ctx, s.security, func(txCtx context.Context) (model.QualityDecision, error) {
		current, err := s.repository.Get(txCtx, id)
		if err != nil {
			return model.QualityDecision{}, err
		}
		if current.Status != "draft" {
			return model.QualityDecision{}, fmt.Errorf("%w: signed quality decision is immutable", ErrInvalidInput)
		}
		if err := validateQualityDecision(current.Code, input.Name, input.HeatCode, input.SampleCode, actor,
			input.Reason, input.DecidedAt, input.Evidence); err != nil {
			return model.QualityDecision{}, err
		}
		heat, sample, err := s.resolveContext(txCtx, input.HeatCode, input.SampleCode)
		if err != nil {
			return model.QualityDecision{}, err
		}
		current.Name = strings.TrimSpace(input.Name)
		current.Description = strings.TrimSpace(input.Description)
		current.HeatCode = heat.Code
		current.SampleCode = sample.Code
		current.Reviewer = strings.TrimSpace(actor)
		current.Reason = strings.TrimSpace(input.Reason)
		current.Conditions = strings.TrimSpace(input.Conditions)
		current.DecidedAt = input.DecidedAt.UTC()
		current.Evidence = strings.TrimSpace(input.Evidence)
		current.Version = input.ExpectedVersion + 1
		current.UpdatedAt = time.Now().UTC()
		if err := s.repository.Update(txCtx, id, input.ExpectedVersion, &current); err != nil {
			return model.QualityDecision{}, fmt.Errorf("update quality decision: %w", err)
		}
		if err := s.security.Audit(txCtx, actor, requestID, "update", "QualityDecision", id, current.Status, current.Status,
			decisionAuditDetail(current)); err != nil {
			return model.QualityDecision{}, fmt.Errorf("persist decision audit: %w", err)
		}
		return s.repository.Get(txCtx, id)
	})
}

func (s *qualityDecisionService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, requestID string) (model.QualityDecision, error) {
	return atomicValue(ctx, s.security, func(txCtx context.Context) (model.QualityDecision, error) {
		current, err := s.repository.Get(txCtx, id)
		if err != nil {
			return model.QualityDecision{}, err
		}
		target := strings.TrimSpace(input.Status)
		// Remelt is not reachable through the generic transition endpoint: it
		// carries a linked heat and therefore has its own closed-loop endpoint.
		if target == string(constants.DecisionTypeRemelt) {
			return model.QualityDecision{}, fmt.Errorf("%w: remelt requires the closed-loop remelt endpoint", ErrInvalidTransition)
		}
		if !constants.CanTransition(constants.QualityDecisionTransitions, current.Status, target) {
			return model.QualityDecision{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
		}
		heat, sample, err := s.resolveContext(txCtx, current.HeatCode, current.SampleCode)
		if err != nil {
			return model.QualityDecision{}, err
		}
		if target == string(constants.DecisionTypeAccept) && (sample.Status != "verified" || !chemistryWithinSpecification(heat, sample)) {
			return model.QualityDecision{}, fmt.Errorf("%w: acceptance requires a verified sample within the heat specification", ErrInvalidInput)
		}
		if exists, duplicateErr := s.repository.HasForHeat(txCtx, heat.Code, current.ID); duplicateErr != nil {
			return model.QualityDecision{}, duplicateErr
		} else if exists {
			return model.QualityDecision{}, fmt.Errorf("%w: heat already has a final quality decision", ErrInvalidInput)
		}
		before := current.Status
		current.Status = target
		current.Reviewer = strings.TrimSpace(actor)
		current.DecidedAt = time.Now().UTC()
		current.Version = input.ExpectedVersion + 1
		current.UpdatedAt = current.DecidedAt
		if err := s.repository.Update(txCtx, id, input.ExpectedVersion, &current); err != nil {
			return model.QualityDecision{}, fmt.Errorf("transition quality decision: %w", err)
		}
		detail := input.Reason + "; " + decisionAuditDetail(current)
		if err := s.security.Audit(txCtx, actor, requestID, "transition", "QualityDecision", id, before, target, detail); err != nil {
			return model.QualityDecision{}, fmt.Errorf("persist decision transition audit: %w", err)
		}

		heatBefore := heat.Status
		if target == string(constants.DecisionTypeAccept) {
			heat.Status = string(constants.HeatStateAccepted)
		} else {
			heat.Status = string(constants.HeatStateRejected)
		}
		heat.Version++
		heat.UpdatedAt = time.Now().UTC()
		if err := s.heats.Update(txCtx, heat.ID, heat.Version-1, &heat); err != nil {
			return model.QualityDecision{}, fmt.Errorf("apply decision to heat: %w", err)
		}
		if err := s.security.Audit(txCtx, actor, requestID, "decision", "Heat", heat.ID, heatBefore, heat.Status,
			"heat state derived from quality decision "+current.Code); err != nil {
			return model.QualityDecision{}, fmt.Errorf("persist derived heat audit: %w", err)
		}
		return s.repository.Get(txCtx, id)
	})
}

// EligibleFurnaces resolves the heat behind a draft decision and returns the
// currently available furnaces covering its grade, charge weight and target
// temperature. It is a read-only projection used to populate the picker; the
// authoritative checks are repeated inside Remelt.
func (s *qualityDecisionService) EligibleFurnaces(ctx context.Context, decisionID uint) ([]dto.RemeltFurnaceOption, error) {
	decision, err := s.repository.Get(ctx, decisionID)
	if err != nil {
		return nil, err
	}
	if decision.Status != model.QualityDecisionInitialStatus {
		return nil, fmt.Errorf("%w: only a draft decision can preview remelt furnaces", ErrInvalidInput)
	}
	heat, err := s.heats.GetByCode(ctx, decision.HeatCode)
	if err != nil {
		return nil, fmt.Errorf("resolve remelt heat: %w", err)
	}
	furnaces, err := s.furnaces.ListEligibleForRemelt(ctx, heat.AlloyGrade, heat.ChargeWeightKg, heat.TargetTemperatureC)
	if err != nil {
		return nil, fmt.Errorf("list eligible furnaces: %w", err)
	}
	options := make([]dto.RemeltFurnaceOption, 0, len(furnaces))
	for _, furnace := range furnaces {
		options = append(options, dto.RemeltFurnaceOption{
			Code: furnace.Code, Name: furnace.Name, PlantArea: furnace.PlantArea, FurnaceType: furnace.FurnaceType,
			CapacityTonnes: furnace.CapacityTonnes, MaxTemperatureC: furnace.MaxTemperatureC,
			Status: furnace.Status, Operator: furnace.Operator,
		})
	}
	return options, nil
}

// Remelt performs the full "判定返炉" closed loop inside one transaction:
//
//  1. Re-read the decision (draft) and the originating heat (hold), enforcing
//     expected-version optimistic locks on both so concurrent submissions fail.
//  2. Reject any duplicate placement (a return heat already linked).
//  3. Re-resolve the chosen furnace and re-run availability + grade/capacity/
//     temperature capability rules against current state (no stale preview).
//  4. Sign the decision to remelt, reject the original heat, create the linked
//     return heat inheriting the frozen chemistry, and reserve the furnace.
//
// Every failure aborts the transaction, leaving heat, furnace and decision
// records untouched.
func (s *qualityDecisionService) Remelt(ctx context.Context, id uint, input dto.RemeltRequest, actor, requestID string) (model.QualityDecision, error) {
	return atomicValue(ctx, s.security, func(txCtx context.Context) (model.QualityDecision, error) {
		current, err := s.repository.Get(txCtx, id)
		if err != nil {
			return model.QualityDecision{}, err
		}
		if current.Status != model.QualityDecisionInitialStatus {
			return model.QualityDecision{}, fmt.Errorf("%w: only a draft decision can be signed as remelt", ErrInvalidTransition)
		}
		if current.RemeltHeatCode != "" {
			return model.QualityDecision{}, fmt.Errorf("%w: decision already placed a return heat", ErrInvalidInput)
		}
		origin, _, err := s.resolveContext(txCtx, current.HeatCode, current.SampleCode)
		if err != nil {
			return model.QualityDecision{}, err
		}
		if exists, dupErr := s.heats.HasRemeltChild(txCtx, origin.Code); dupErr != nil {
			return model.QualityDecision{}, dupErr
		} else if exists {
			return model.QualityDecision{}, fmt.Errorf("%w: original heat already has a return heat", ErrInvalidInput)
		}
		// The decision's expected version and the heat's current version are
		// both checked at write time via optimistic-lock updates, which rejects
		// a concurrent remelt that raced ahead.
		furnace, err := s.resolveRemeltFurnace(txCtx, strings.TrimSpace(input.FurnaceCode), origin)
		if err != nil {
			return model.QualityDecision{}, err
		}
		returnHeat, err := buildReturnHeat(input, origin, furnace.Code)
		if err != nil {
			return model.QualityDecision{}, err
		}
		// Reject a duplicate return-heat code up front with a clear reason
		// rather than surfacing a unique-index constraint.
		if _, lookupErr := s.heats.GetByCode(txCtx, returnHeat.Code); lookupErr == nil {
			return model.QualityDecision{}, fmt.Errorf("%w: return heat code already exists", ErrInvalidInput)
		}

		// 1) Sign the decision to remelt.
		decisionBefore := current.Status
		now := time.Now().UTC()
		current.Status = string(constants.DecisionTypeRemelt)
		current.Reviewer = strings.TrimSpace(actor)
		current.DecidedAt = now
		current.RemeltHeatCode = returnHeat.Code
		current.RemeltFurnaceCode = furnace.Code
		current.RemeltedAt = &now
		current.Version = input.ExpectedVersion + 1
		current.UpdatedAt = now
		if err := s.repository.Update(txCtx, id, input.ExpectedVersion, &current); err != nil {
			return model.QualityDecision{}, fmt.Errorf("sign remelt decision: %w", err)
		}
		decisionDetail := strings.TrimSpace(input.Reason) + "; " + decisionAuditDetail(current) +
			fmt.Sprintf("; returnHeat=%s furnace=%s", returnHeat.Code, furnace.Code)
		if err := s.security.Audit(txCtx, actor, requestID, "remelt", "QualityDecision", id, decisionBefore,
			current.Status, decisionDetail); err != nil {
			return model.QualityDecision{}, fmt.Errorf("persist remelt decision audit: %w", err)
		}

		// 2) Reject the original heat.
		originBefore := origin.Status
		origin.Status = string(constants.HeatStateRejected)
		origin.RemeltedIntoCode = returnHeat.Code
		origin.Version++
		origin.UpdatedAt = now
		if err := s.heats.Update(txCtx, origin.ID, origin.Version-1, &origin); err != nil {
			return model.QualityDecision{}, fmt.Errorf("reject original heat: %w", err)
		}
		if err := s.security.Audit(txCtx, actor, requestID, "remelt", "Heat", origin.ID, originBefore,
			origin.Status, "original heat rejected; carried to return heat "+returnHeat.Code); err != nil {
			return model.QualityDecision{}, fmt.Errorf("persist original heat audit: %w", err)
		}

		// 3) Open the linked return heat, inheriting the frozen chemistry.
		if err := s.heats.Create(txCtx, &returnHeat); err != nil {
			return model.QualityDecision{}, fmt.Errorf("create return heat: %w", err)
		}
		if err := s.security.Audit(txCtx, actor, requestID, "remelt", "Heat", returnHeat.ID, "", returnHeat.Status,
			"return heat opened from "+origin.Code+" on "+furnace.Code+"; "+heatAuditDetail(returnHeat)); err != nil {
			return model.QualityDecision{}, fmt.Errorf("persist return heat audit: %w", err)
		}

		// 4) Reserve the receiving furnace (available -> charging).
		furnaceBefore := furnace.Status
		furnace.Status = "charging"
		furnace.Version++
		furnace.UpdatedAt = now
		if err := s.furnaces.Update(txCtx, furnace.ID, furnace.Version-1, &furnace); err != nil {
			return model.QualityDecision{}, fmt.Errorf("reserve remelt furnace: %w", err)
		}
		if err := s.security.Audit(txCtx, actor, requestID, "remelt", "Furnace", furnace.ID, furnaceBefore,
			furnace.Status, "furnace reserved for return heat "+returnHeat.Code); err != nil {
			return model.QualityDecision{}, fmt.Errorf("persist furnace reservation audit: %w", err)
		}

		return s.repository.Get(txCtx, id)
	})
}

// resolveRemeltFurnace re-runs the capability rules against current furnace
// state. The preview list is advisory; the closed loop must not trust it.
func (s *qualityDecisionService) resolveRemeltFurnace(ctx context.Context, furnaceCode string, origin model.Heat) (model.Furnace, error) {
	furnace, err := s.furnaces.GetByCode(ctx, normalizeCode(furnaceCode))
	if err != nil {
		return model.Furnace{}, fmt.Errorf("resolve remelt furnace: %w", err)
	}
	if furnace.Status != "available" {
		return model.Furnace{}, fmt.Errorf("%w: furnace %s is not currently available (status=%s)", ErrInvalidInput, furnace.Code, furnace.Status)
	}
	if origin.ChargeWeightKg > furnace.CapacityTonnes*1000 {
		return model.Furnace{}, fmt.Errorf("%w: charge weight %.0fkg exceeds furnace %s capacity %.1ft", ErrInvalidInput, origin.ChargeWeightKg, furnace.Code, furnace.CapacityTonnes)
	}
	if origin.TargetTemperatureC > furnace.MaxTemperatureC {
		return model.Furnace{}, fmt.Errorf("%w: target temperature %.0fC exceeds furnace %s limit %.0fC", ErrInvalidInput, origin.TargetTemperatureC, furnace.Code, furnace.MaxTemperatureC)
	}
	if !alloySupported(furnace.SupportedAlloys, origin.AlloyGrade) {
		return model.Furnace{}, fmt.Errorf("%w: furnace %s does not support alloy grade %s", ErrInvalidInput, furnace.Code, origin.AlloyGrade)
	}
	return furnace, nil
}

// buildReturnHeat constructs the linked heat that carries the rejected charge
// forward. Chemistry specification (C/Si/S/P limits), grade, weight and target
// temperature are inherited verbatim from the origin; only the identity,
// furnace, owner, evidence and start time describe the new charging cycle.
func buildReturnHeat(input dto.RemeltRequest, origin model.Heat, furnaceCode string) (model.Heat, error) {
	code := normalizeCode(input.ReturnHeatCode)
	name := strings.TrimSpace(input.ReturnHeatName)
	if code == "" || name == "" {
		return model.Heat{}, fmt.Errorf("%w: return heat code and name are required", ErrInvalidInput)
	}
	if code == origin.Code {
		return model.Heat{}, fmt.Errorf("%w: return heat code must differ from the original heat", ErrInvalidInput)
	}
	owner := strings.TrimSpace(input.Owner)
	if owner == "" {
		owner = origin.Owner
	}
	evidence := strings.TrimSpace(input.Evidence)
	if evidence == "" {
		return model.Heat{}, fmt.Errorf("%w: return heat charging evidence is required", ErrInvalidInput)
	}
	now := time.Now().UTC()
	return model.Heat{
		BaseModel: model.BaseModel{
			Code: code, Name: name, Status: model.HeatInitialStatus, Version: 1,
			Description: fmt.Sprintf("返炉承接炉次，继承自 %s；%s", origin.Code, strings.TrimSpace(input.Reason)),
		},
		FurnaceCode:        furnaceCode,
		AlloyGrade:         origin.AlloyGrade,
		Owner:              owner,
		ChargeWeightKg:     origin.ChargeWeightKg,
		TargetTemperatureC: origin.TargetTemperatureC,
		CarbonMinPct:       origin.CarbonMinPct,
		CarbonMaxPct:       origin.CarbonMaxPct,
		SiliconMinPct:      origin.SiliconMinPct,
		SiliconMaxPct:      origin.SiliconMaxPct,
		SulfurMaxPct:       origin.SulfurMaxPct,
		PhosphorusMaxPct:   origin.PhosphorusMaxPct,
		StartedAt:          now,
		Evidence:           evidence,
		RemeltOfCode:       origin.Code,
	}, nil
}

func (s *qualityDecisionService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	return atomicError(ctx, s.security, func(txCtx context.Context) error {
		current, err := s.repository.Get(txCtx, id)
		if err != nil {
			return err
		}
		if current.Status != "draft" {
			return fmt.Errorf("%w: only a draft decision can be deleted", ErrInvalidInput)
		}
		if err := s.repository.Delete(txCtx, id); err != nil {
			return err
		}
		return s.security.Audit(txCtx, actor, requestID, "delete", "QualityDecision", id, current.Status, "deleted", "draft decision soft deleted")
	})
}

func (s *qualityDecisionService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func (s *qualityDecisionService) resolveContext(ctx context.Context, heatCode, sampleCode string) (model.Heat, model.ChemicalSample, error) {
	heat, err := s.heats.GetByCode(ctx, normalizeCode(heatCode))
	if err != nil {
		return model.Heat{}, model.ChemicalSample{}, fmt.Errorf("resolve decision heat: %w", err)
	}
	if heat.Status != string(constants.HeatStateHold) {
		return model.Heat{}, model.ChemicalSample{}, fmt.Errorf("%w: heat must be on quality hold before a decision", ErrInvalidInput)
	}
	// A heat already carried into a return heat cannot be decided again.
	if heat.RemeltedIntoCode != "" {
		return model.Heat{}, model.ChemicalSample{}, fmt.Errorf("%w: heat was already carried to return heat %s", ErrInvalidInput, heat.RemeltedIntoCode)
	}
	sample, err := s.samples.GetByCode(ctx, normalizeCode(sampleCode))
	if err != nil {
		return model.Heat{}, model.ChemicalSample{}, fmt.Errorf("resolve decision sample: %w", err)
	}
	if sample.HeatCode != heat.Code {
		return model.Heat{}, model.ChemicalSample{}, fmt.Errorf("%w: chemical sample belongs to another heat", ErrInvalidInput)
	}
	if sample.Status != "verified" && sample.Status != "rejected" {
		return model.Heat{}, model.ChemicalSample{}, fmt.Errorf("%w: chemical sample must be terminal before a decision", ErrInvalidInput)
	}
	return heat, sample, nil
}

func validateQualityDecision(code, name, heatCode, sampleCode, reviewer, reason string, decidedAt time.Time, evidence string) error {
	for _, value := range []string{code, name, heatCode, sampleCode, reviewer, reason, evidence} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: decision identity, heat, sample, reviewer, reason and evidence are required", ErrInvalidInput)
		}
	}
	if decidedAt.IsZero() || decidedAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return fmt.Errorf("%w: decision time is invalid", ErrInvalidInput)
	}
	return nil
}

func chemistryWithinSpecification(heat model.Heat, sample model.ChemicalSample) bool {
	return sample.IsPlausible() &&
		sample.CarbonPct >= heat.CarbonMinPct && sample.CarbonPct <= heat.CarbonMaxPct &&
		sample.SiliconPct >= heat.SiliconMinPct && sample.SiliconPct <= heat.SiliconMaxPct &&
		sample.SulfurPct <= heat.SulfurMaxPct && sample.PhosphorusPct <= heat.PhosphorusMaxPct
}

func decisionAuditDetail(item model.QualityDecision) string {
	return fmt.Sprintf("heat=%s sample=%s reviewer=%s reason=%s", item.HeatCode, item.SampleCode, item.Reviewer, item.Reason)
}
