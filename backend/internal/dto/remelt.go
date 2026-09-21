package dto

// RemeltHandover is the single-submission contract for the return-to-furnace
// closed loop. Signing a decision as remelt must simultaneously reject the
// original heat, reserve the承接 furnace and generate the linked return heat,
// so the generic transition endpoint deliberately cannot express this action.
type RemeltHandover struct {
	ExpectedVersion uint   `json:"expectedVersion" binding:"required"`
	FurnaceCode     string `json:"furnaceCode" binding:"required,max=64"`
	ReturnHeatCode  string `json:"returnHeatCode" binding:"required,min=2,max=64"`
	ReturnHeatName  string `json:"returnHeatName" binding:"required,min=2,max=160"`
	Reason          string `json:"reason" binding:"required,min=3,max=500"`
	Evidence        string `json:"evidence" binding:"required,min=3,max=2000"`
}

// RemeltFurnaceOption describes one furnace that can currently承接 a remelt,
// including the capability mismatch reasons for the excluded furnaces.
type RemeltFurnaceOption struct {
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	Status          string   `json:"status"`
	PlantArea       string   `json:"plantArea"`
	CapacityTonnes  float64  `json:"capacityTonnes"`
	MaxTemperatureC float64  `json:"maxTemperatureC"`
	Eligible        bool     `json:"eligible"`
	Reasons         []string `json:"reasons"`
}
