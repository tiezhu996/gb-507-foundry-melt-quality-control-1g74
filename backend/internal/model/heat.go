package model

import "time"

// Heat binds a melt to a furnace and an explicit chemistry specification.
//
// Remelt lineage closes the "判定返炉" loop: when a quality decision signs off
// as remelt, the original heat is rejected and a linked heat is reopened on a
// compliant furnace. The child inherits the frozen chemistry specification.
//
//   - RemeltOfCode is empty for an ordinary heat and points to the originating
//     (rejected) heat code for a return heat; it is immutable once set.
//   - RemeltedIntoCode is empty until a remelt decision derives a child heat,
//     then it records the code of the heat that carries the charge forward.
type Heat struct {
	BaseModel
	FurnaceCode        string    `json:"furnaceCode" gorm:"size:64;index;not null"`
	AlloyGrade         string    `json:"alloyGrade" gorm:"size:80;index;not null"`
	Owner              string    `json:"owner" gorm:"size:120;index;not null"`
	ChargeWeightKg     float64   `json:"chargeWeightKg" gorm:"not null"`
	TargetTemperatureC float64   `json:"targetTemperatureC" gorm:"not null"`
	CarbonMinPct       float64   `json:"carbonMinPct" gorm:"not null"`
	CarbonMaxPct       float64   `json:"carbonMaxPct" gorm:"not null"`
	SiliconMinPct      float64   `json:"siliconMinPct" gorm:"not null"`
	SiliconMaxPct      float64   `json:"siliconMaxPct" gorm:"not null"`
	SulfurMaxPct       float64   `json:"sulfurMaxPct" gorm:"not null"`
	PhosphorusMaxPct   float64   `json:"phosphorusMaxPct" gorm:"not null"`
	StartedAt          time.Time `json:"startedAt" gorm:"index;not null"`
	Evidence           string    `json:"evidence" gorm:"size:2000"`
	RemeltOfCode       string    `json:"remeltOfCode" gorm:"size:64;index"`
	RemeltedIntoCode   string    `json:"remeltedIntoCode" gorm:"size:64;index"`
}

func (item *Heat) GetBase() *BaseModel { return &item.BaseModel }

func (item Heat) TableName() string { return "heats" }

const HeatInitialStatus = "charged"

// IsRemelt reports whether the heat was reopened from a rejected heat.
func (item *Heat) IsRemelt() bool { return item.RemeltOfCode != "" }
