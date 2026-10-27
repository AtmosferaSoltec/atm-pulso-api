package domain

// PlanLimits is the allowance before an explicit extra agreement is required.
type PlanLimits struct{ MaxGyms, MaxBranchesPerGym int }

// FreePlan is the single definition of the free allowance.
var FreePlan = PlanLimits{MaxGyms: 1, MaxBranchesPerGym: 1}

// Extras counts resources beyond the free allowance.
func Extras(branchCountPerGym []int) (extraGyms, extraBranches int) {
	extraGyms = max(0, len(branchCountPerGym)-FreePlan.MaxGyms)
	for _, count := range branchCountPerGym {
		extraBranches += max(0, count-FreePlan.MaxBranchesPerGym)
	}
	return
}

// DefaultDayPassPrice is used when a gym has no configured price.
const DefaultDayPassPrice = "15.00"
