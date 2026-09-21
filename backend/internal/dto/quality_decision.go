package dto

import "time"

// CreateQualityDecision is the public write contract for 质量决定. Status is deliberately
// omitted so callers cannot bypass the service state machine.
type CreateQualityDecision struct {
	Code        string    `json:"code" binding:"required,min=2,max=64"`
	Name        string    `json:"name" binding:"required,min=2,max=160"`
	Description string    `json:"description" binding:"max=1000"`
	HeatCode    string    `json:"heatCode" binding:"required,max=64"`
	SampleCode  string    `json:"sampleCode" binding:"required,max=64"`
	Reviewer    string    `json:"reviewer" binding:"max=120"`
	Reason      string    `json:"reason" binding:"required,min=3,max=1000"`
	Conditions  string    `json:"conditions" binding:"max=1000"`
	DecidedAt   time.Time `json:"decidedAt" binding:"required"`
	Evidence    string    `json:"evidence" binding:"required,min=3,max=2000"`
}

type UpdateQualityDecision struct {
	ExpectedVersion uint `json:"expectedVersion" binding:"required"`
	CreateQualityDecision
}

// RemeltRequest carries the "判定返炉" closed loop. Signing a remelt decision
// must, in a single submission, reject the original heat and open a linked
// return heat on a furnace that supports the original alloy grade, capacity
// and target temperature. The return heat code is caller supplied so the new
// heat has a stable, human-facing identifier.
type RemeltRequest struct {
	// ExpectedVersion guards the decision against concurrent signing.
	ExpectedVersion uint `json:"expectedVersion" binding:"required"`
	// Reason is recorded on the decision transition and on every derived audit.
	Reason string `json:"reason" binding:"required,min=3,max=500"`
	// FurnaceCode identifies the compliant receiving furnace.
	FurnaceCode string `json:"furnaceCode" binding:"required,max=64"`
	// ReturnHeatCode is the code of the newly opened linked heat.
	ReturnHeatCode string `json:"returnHeatCode" binding:"required,min=2,max=64"`
	// ReturnHeatName is the human-facing name of the return heat.
	ReturnHeatName string `json:"returnHeatName" binding:"required,min=2,max=160"`
	// Owner may be reassigned; when blank the original heat owner is inherited.
	Owner string `json:"owner" binding:"max=120"`
	// Evidence for the new charging cycle, distinct from the original MES record.
	Evidence string `json:"evidence" binding:"required,min=3,max=2000"`
}

// RemeltFurnaceOption describes one furnace currently eligible to receive a
// rejected heat, computed against the heat's frozen grade/capacity/temperature.
type RemeltFurnaceOption struct {
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	PlantArea       string  `json:"plantArea"`
	FurnaceType     string  `json:"furnaceType"`
	CapacityTonnes  float64 `json:"capacityTonnes"`
	MaxTemperatureC float64 `json:"maxTemperatureC"`
	Status          string  `json:"status"`
	Operator        string  `json:"operator"`
}
