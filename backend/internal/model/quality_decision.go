package model

import "time"

// QualityDecision is the immutable sign-off tying a heat to laboratory evidence.
//
// A remelt decision does not finish at "rejected": it carries the charge to a
// linked return heat. The acceptance fields persist that closed loop so the
// decision page can read it back after refresh:
//
//   - RemeltHeatCode: code of the newly opened heat that received the charge
//     (empty for draft/accept/scrap and for a remelt that could not be placed).
//   - RemeltFurnaceCode: the compliant furnace the return heat was charged on.
//   - RemeltedAt: when the closed loop completed.
type QualityDecision struct {
	BaseModel
	HeatCode          string     `json:"heatCode" gorm:"size:64;uniqueIndex;not null"`
	SampleCode        string     `json:"sampleCode" gorm:"size:64;index;not null"`
	Reviewer          string     `json:"reviewer" gorm:"size:120;index;not null"`
	Reason            string     `json:"reason" gorm:"size:1000;not null"`
	Conditions        string     `json:"conditions" gorm:"size:1000"`
	DecidedAt         time.Time  `json:"decidedAt" gorm:"index;not null"`
	Evidence          string     `json:"evidence" gorm:"size:2000;not null"`
	RemeltHeatCode    string     `json:"remeltHeatCode" gorm:"size:64;index"`
	RemeltFurnaceCode string     `json:"remeltFurnaceCode" gorm:"size:64;index"`
	RemeltedAt        *time.Time `json:"remeltedAt" gorm:"index"`
}

func (item *QualityDecision) GetBase() *BaseModel { return &item.BaseModel }

func (item QualityDecision) TableName() string { return "quality_decisions" }

const QualityDecisionInitialStatus = "draft"

// HasRemeltLink reports whether a return heat has been derived from the decision.
func (item *QualityDecision) HasRemeltLink() bool { return item.RemeltHeatCode != "" }
