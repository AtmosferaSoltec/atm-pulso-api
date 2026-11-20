package export

import (
	"bytes"
	"context"

	"github.com/google/uuid"
)

// BrandSource gives the letterhead of a gym (platform/gymbrand implements it).
type BrandSource interface {
	Brand(ctx context.Context, gymID, tenantID uuid.UUID) (Brand, error)
}

// BrandOf asks the source for the gym's brand and never fails: without a source, or when it errs, the PDF
// is still produced with Pulso's colour and a neutral name.
func BrandOf(ctx context.Context, source BrandSource, gymID, tenantID uuid.UUID) Brand {
	fallback := Brand{Name: "Pulso", Color: DefaultColor}
	if source == nil {
		return fallback
	}
	brand, err := source.Brand(ctx, gymID, tenantID)
	if err != nil {
		return fallback
	}
	if brand.Name == "" {
		brand.Name = fallback.Name
	}
	return brand
}

// Render writes the report in the format to memory.
func Render(f Format, r Report) ([]byte, error) {
	var buf bytes.Buffer
	if err := Write(&buf, f, r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
