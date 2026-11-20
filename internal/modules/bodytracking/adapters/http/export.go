package http

import (
	"bytes"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/unicode/norm"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/adapters/pdf"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/export"
)

// ExportHandler serves GET /members/{id}/measurements/pdf.
type ExportHandler struct {
	service *app.Service
	brands  export.BrandSource
}

// NewExportHandler builds the handler; brands may be nil (Pulso's letterhead).
func NewExportHandler(service *app.Service, brands export.BrandSource) *ExportHandler {
	return &ExportHandler{service: service, brands: brands}
}

// Routes mounts the PDF behind the same module guard as every measurement route and the per-IP limit of heavy
// requests.
func (h *ExportHandler) Routes(protected gin.IRoutes, limit gin.HandlerFunc) {
	protected.GET("/members/:id/measurements/pdf", auth.RequireFeature(auth.FeatureBodyTracking), limit, h.measurementsPDF)
}

func (h *ExportHandler) measurementsPDF(c *gin.Context) {
	member, ok := idParam(c, "id")
	if !ok {
		return
	}
	p := principal(c)
	report, err := h.service.MeasurementReport(c.Request.Context(), p, member)
	if err != nil {
		respond(c, err)
		return
	}
	now := h.service.Clock.Now()
	brand := export.BrandOf(c.Request.Context(), h.brands, p.GymID, p.TenantID)
	var buf bytes.Buffer
	if err := pdf.Render(&buf, brand, now, report); err != nil {
		apierror.Respond(c, err)
		return
	}
	name := "medidas-" + slug(report.Member.FullName)
	c.Header("Content-Disposition", export.Disposition(export.Filename(name, clock.LimaDate(now), export.PDF)))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, export.PDF.ContentType(), buf.Bytes())
}

// slug turns "José Ñañez" into "jose-nanez" (ASCII, for a file name); empty names give "miembro".
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.ToLower(name)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimSuffix(b.String(), "-")
	if len(out) > 60 {
		out = strings.TrimSuffix(out[:60], "-")
	}
	if out == "" {
		return "miembro"
	}
	return out
}
