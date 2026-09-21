package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/foundry-melt-quality-control/backend/internal/constants"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/dto"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/model"
	"github.com/blueship581/foundry-melt-quality-control/backend/internal/repository"
)

// RemeltHandoverResult is the readable confirmation of one remelt submission.
// All four aggregates are returned so the UI can render the closed loop without
// issuing follow-up requests.
type RemeltHandoverResult struct {
	Decision   model.QualityDecision `json:"decision"`
	OriginHeat model.Heat            `json:"originHeat"`
	ReturnHeat model.Heat            `json:"returnHeat"`
	Furnace    model.Furnace         `json:"furnace"`
}

// RemeltHandover executes the return-to-furnace closed loop in one transaction:
// reject the original heat, sign the decision as remelt, create the linked
// return heat that inherits the frozen chemistry specification and reserve the
// compliant承接 furnace. Any failure rolls back every aggregate write.
func (s *qualityDecisionService) RemeltHandover(ctx context.Context, id uint, input dto.RemeltHandover, actor, requestID string) (RemeltHandoverResult, error) {
	result := RemeltHandoverResult{}
	err := atomicError(ctx, s.security, func(txCtx context.Context) error {
		decision, err := s.repository.Get(txCtx, id)
		if err != nil {
			return err
		}
		if decision.Version != input.ExpectedVersion {
			return fmt.Errorf("%w: quality decision %s version changed (expected %d, stored %d); refresh and retry",
				repository.ErrVersionConflict, decision.Code, input.ExpectedVersion, decision.Version)
		}
		if decision.Status != model.QualityDecisionInitialStatus {
			return fmt.Errorf("%w: only a draft quality decision can be signed; decision %s is already %s",
				ErrInvalidTransition, decision.Code, decision.Status)
		}
		if strings.TrimSpace(decision.RemeltHeatCode) != "" {
			return fmt.Errorf("%w: decision %s has already produced return heat %s; duplicate remelt is rejected",
				ErrInvalidInput, decision.Code, decision.RemeltHeatCode)
		}

		// resolveContext enforces that the heat is still on quality hold and the
		// terminal sample belongs to it, so a concurrently finalized heat fails here.
		heat, _, err := s.resolveContext(txCtx, decision.HeatCode, decision.SampleCode)
		if err != nil {
			return err
		}

		returnCode := normalizeCode(input.ReturnHeatCode)
		returnName := strings.TrimSpace(input.ReturnHeatName)
		if returnCode == "" || returnName == "" || strings.TrimSpace(input.FurnaceCode) == "" ||
			strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.Evidence) == "" {
			return fmt.Errorf("%w: return heat identity, target furnace, reason and evidence are required", ErrInvalidInput)
		}
		if codeExists, err := s.heats.ExistsByCode(txCtx, returnCode); err != nil {
			return err
		} else if codeExists {
			return fmt.Errorf("%w: return heat code %s already exists", ErrInvalidInput, returnCode)
		}

		furnace, err := s.furnaces.GetByCode(txCtx, normalizeCode(input.FurnaceCode))
		if err != nil {
			return fmt.Errorf("resolve remelt furnace: %w", err)
		}
		if err := validateRemeltFurnace(furnace, heat); err != nil {
			return err
		}

		// Conditional claim: a concurrent request taking the same furnace loses the
		// race here and the whole transaction rolls back without side effects.
		claimed, err := s.furnaces.ClaimAvailable(txCtx, furnace.ID, furnace.Version)
		if err != nil {
			if errors.Is(err, repository.ErrFurnaceNotAvailable) {
				// ClaimAvailable returns the current furnace on a failed claim so the
				// caller can report the precise reason to the operator.
				return fmt.Errorf("%w: furnace %s is no longer available (current status=%s); refresh the candidate list and retry with another furnace",
					repository.ErrVersionConflict, furnace.Code, claimed.Status)
			}
			if errors.Is(err, repository.ErrVersionConflict) {
				return fmt.Errorf("%w: furnace %s is being claimed by another concurrent request; refresh the candidate list and retry",
					repository.ErrVersionConflict, furnace.Code)
			}
			return fmt.Errorf("claim remelt furnace: %w", err)
		}

		now := time.Now().UTC()
		returnHeat := model.Heat{
			BaseModel: model.BaseModel{
				Code: returnCode, Name: returnName, Status: model.HeatInitialStatus, Version: 1,
				Description: fmt.Sprintf("返炉承接炉次，承接自原炉次 %s，质量决定 %s", heat.Code, decision.Code),
			},
			FurnaceCode: claimed.Code, AlloyGrade: heat.AlloyGrade, Owner: heat.Owner,
			ChargeWeightKg: heat.ChargeWeightKg, TargetTemperatureC: heat.TargetTemperatureC,
			CarbonMinPct: heat.CarbonMinPct, CarbonMaxPct: heat.CarbonMaxPct,
			SiliconMinPct: heat.SiliconMinPct, SiliconMaxPct: heat.SiliconMaxPct,
			SulfurMaxPct: heat.SulfurMaxPct, PhosphorusMaxPct: heat.PhosphorusMaxPct,
			StartedAt: now, Evidence: strings.TrimSpace(input.Evidence),
			OriginHeatCode: heat.Code,
		}
		if err := s.heats.Create(txCtx, &returnHeat); err != nil {
			return fmt.Errorf("create return heat: %w", err)
		}

		heatBefore := heat.Status
		heat.Status = string(constants.HeatStateRejected)
		heat.ReturnedHeatCode = returnHeat.Code
		heat.Version++
		heat.UpdatedAt = now
		if err := s.heats.Update(txCtx, heat.ID, heat.Version-1, &heat); err != nil {
			if errors.Is(err, repository.ErrVersionConflict) {
				return fmt.Errorf("%w: original heat %s was changed concurrently; refresh and retry",
					repository.ErrVersionConflict, heat.Code)
			}
			return fmt.Errorf("reject original heat: %w", err)
		}

		before := decision.Status
		decision.Status = string(constants.DecisionTypeRemelt)
		decision.RemeltFurnaceCode = claimed.Code
		decision.RemeltHeatCode = returnHeat.Code
		decision.Reviewer = strings.TrimSpace(actor)
		decision.DecidedAt = now
		decision.Version = input.ExpectedVersion + 1
		decision.UpdatedAt = now
		if err := s.repository.Update(txCtx, id, input.ExpectedVersion, &decision); err != nil {
			if errors.Is(err, repository.ErrVersionConflict) {
				return fmt.Errorf("%w: quality decision %s was signed concurrently; refresh and retry",
					repository.ErrVersionConflict, decision.Code)
			}
			return fmt.Errorf("sign remelt decision: %w", err)
		}

		// Audits are mandatory: failing any of them rolls back the business writes.
		if err := s.security.Audit(txCtx, actor, requestID, "transition", "Furnace", claimed.ID, "available", "charging",
			fmt.Sprintf("reserved for return heat %s of original heat %s via decision %s", returnHeat.Code, heat.Code, decision.Code)); err != nil {
			return fmt.Errorf("persist furnace claim audit: %w", err)
		}
		if err := s.security.Audit(txCtx, actor, requestID, "create", "Heat", returnHeat.ID, "", returnHeat.Status,
			"return heat created by remelt decision "+decision.Code+"; "+heatAuditDetail(returnHeat)); err != nil {
			return fmt.Errorf("persist return heat audit: %w", err)
		}
		if err := s.security.Audit(txCtx, actor, requestID, "decision", "Heat", heat.ID, heatBefore, heat.Status,
			fmt.Sprintf("original heat rejected by remelt decision %s; return heat %s on furnace %s", decision.Code, returnHeat.Code, claimed.Code)); err != nil {
			return fmt.Errorf("persist original heat audit: %w", err)
		}
		if err := s.security.Audit(txCtx, actor, requestID, "transition", "QualityDecision", id, before, decision.Status,
			fmt.Sprintf("%s; remelt to furnace %s, return heat %s; %s",
				strings.TrimSpace(input.Reason), claimed.Code, returnHeat.Code, decisionAuditDetail(decision))); err != nil {
			return fmt.Errorf("persist remelt decision audit: %w", err)
		}

		result = RemeltHandoverResult{Decision: decision, OriginHeat: heat, ReturnHeat: returnHeat, Furnace: claimed}
		return nil
	})
	return result, err
}

// validateRemeltFurnace encodes the closed-loop eligibility: the承接 furnace
// must currently be available and match the original alloy grade, charge weight
// and target temperature. Every failure carries the specific reason.
func validateRemeltFurnace(furnace model.Furnace, heat model.Heat) error {
	if furnace.Status != "available" {
		return fmt.Errorf("%w: furnace %s is not available (current status=%s); only an available furnace can承接 the remelt",
			ErrInvalidInput, furnace.Code, furnace.Status)
	}
	if heat.ChargeWeightKg > furnace.CapacityTonnes*1000 {
		return fmt.Errorf("%w: furnace %s capacity %.1ft cannot hold the original charge %.0fkg",
			ErrInvalidInput, furnace.Code, furnace.CapacityTonnes, heat.ChargeWeightKg)
	}
	if heat.TargetTemperatureC > furnace.MaxTemperatureC {
		return fmt.Errorf("%w: furnace %s max temperature %.0fC is below the original target %.0fC",
			ErrInvalidInput, furnace.Code, furnace.MaxTemperatureC, heat.TargetTemperatureC)
	}
	if !alloySupported(furnace.SupportedAlloys, heat.AlloyGrade) {
		return fmt.Errorf("%w: furnace %s does not support alloy grade %s",
			ErrInvalidInput, furnace.Code, heat.AlloyGrade)
	}
	return nil
}
