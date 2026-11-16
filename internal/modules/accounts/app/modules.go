package app

import "github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"

// Module kinds. Base: every gym has it and it cannot be switched off. Optional: the platform superadmin switches it
// on per gym.
const (
	ModuleBase     = "base"
	ModuleOptional = "optional"
)

// Module is an entry of the catalog the console shows. The console names each code in Spanish.
type Module struct {
	Code string `json:"code"`
	Kind string `json:"kind"`
	// Switchable: it is an optional module the superadmin turns on with PATCH /admin/gyms/{id}/features.
	Switchable bool `json:"switchable"`
	// Requires are the codes that must be on first. Empty for every module today.
	Requires []string `json:"requires"`
}

// coreModules are part of every gym and are not features: no route checks them.
var coreModules = []string{"members", "payments", "access", "reports", "team"}

// Modules is the catalog: the core of Pulso, then every feature in the order of auth.AllFeatures.
func Modules() []Module {
	out := make([]Module, 0, len(coreModules)+len(auth.AllFeatures))
	for _, code := range coreModules {
		out = append(out, Module{Code: code, Kind: ModuleBase, Requires: []string{}})
	}
	for _, f := range auth.AllFeatures {
		kind := ModuleOptional
		if f.IsBase() {
			kind = ModuleBase
		}
		out = append(out, Module{Code: string(f), Kind: kind, Switchable: !f.IsBase(), Requires: []string{}})
	}
	return out
}
