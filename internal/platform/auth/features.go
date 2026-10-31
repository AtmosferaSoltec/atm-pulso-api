package auth

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
)

// Feature is an optional module a gym has (or has not) switched on. Only the platform superadmin
// switches them, from console-web (PATCH /admin/gyms/{id}/features). The list is closed: a new module
// is added here, in this one place, and nowhere else.
type Feature string

// The closed list of modules (plan 08).
const (
	// FeatureSales: sales of products (paid now or pending collection) and the product catalog.
	FeatureSales Feature = "sales"
	// FeatureBodyTracking: body measurements and routines. Health data: audited.
	FeatureBodyTracking Feature = "body_tracking"
	// FeatureKiosk: the access tablet (kiosk mode).
	FeatureKiosk Feature = "kiosk"
)

// AllFeatures is the closed list, in the order they are shown.
var AllFeatures = []Feature{FeatureSales, FeatureBodyTracking, FeatureKiosk}

// Valid says whether f is one of the known modules.
func (f Feature) Valid() bool { return slices.Contains(AllFeatures, f) }

// NormalizeFeatures validates a list of module names and returns it without repeats, in the order of
// AllFeatures. It reports false when any name is unknown.
func NormalizeFeatures(raw []string) ([]Feature, bool) {
	wanted := make(map[Feature]bool, len(raw))
	for _, name := range raw {
		f := Feature(name)
		if !f.Valid() {
			return nil, false
		}
		wanted[f] = true
	}
	out := make([]Feature, 0, len(wanted))
	for _, f := range AllFeatures {
		if wanted[f] {
			out = append(out, f)
		}
	}
	return out, true
}

// HasFeature says whether the person's gym has the module switched on.
func (p Principal) HasFeature(f Feature) bool { return slices.Contains(p.Features, f) }

// CodeFeatureDisabled is the stable error code of a module the gym does not have.
const CodeFeatureDisabled = "FEATURE_DISABLED"

var errFeatureDisabled = apierror.New(http.StatusForbidden, CodeFeatureDisabled, "Esta función no está activada para tu gimnasio.")

// RequireFeature is the reusable guard of an optional module: mount it on the module's routes, after
// the authentication guard, and every request of a gym without the module answers 403 FEATURE_DISABLED.
//
//	protected.GET("/products", auth.RequireFeature(auth.FeatureSales), h.list)
func RequireFeature(f Feature) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := FromContext(c.Request.Context())
		if !ok || !p.HasFeature(f) {
			apierror.Respond(c, errFeatureDisabled)
			return
		}
		c.Next()
	}
}
