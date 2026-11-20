package pdf

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/app"
)

func TestTableRowsOneRowPerDateAfterMerge(t *testing.T) {
	n := func(s string) *json.Number { v := json.Number(s); return &v }
	t0 := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	list := []app.MeasurementView{
		{MeasuredOn: "2026-05-10", CreatedAt: t0.Add(5 * time.Minute), HeightCm: n("175.0"), WaistCm: n("90.0")},
		{MeasuredOn: "2026-05-10", CreatedAt: t0, WeightKg: n("80.0")},
		{MeasuredOn: "2026-05-09", CreatedAt: t0, WeightKg: n("81.0")},
	}
	rows := tableRows(app.MergeByDay(list))
	assert.Len(t, rows, 2)
	assert.Equal(t, []string{dateOf("2026-05-10"), "80.0", "175.0", "—", "90.0", "—", "—"}, rows[0])
	assert.Equal(t, dateOf("2026-05-09"), rows[1][0])
}
