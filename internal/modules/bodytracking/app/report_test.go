package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/app"
)

var base = time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)

func num(s string) *json.Number { n := json.Number(s); return &n }

func rec(day string, minutes int, f func(*app.MeasurementView)) app.MeasurementView {
	m := app.MeasurementView{MeasuredOn: day, CreatedAt: base.Add(time.Duration(minutes) * time.Minute)}
	f(&m)
	return m
}

func TestMergeByDayWeightThenHeightIsOneRow(t *testing.T) {
	list := []app.MeasurementView{ // newest first
		rec("2026-05-10", 10, func(m *app.MeasurementView) { m.HeightCm, m.WaistCm = num("170.0"), num("80.0") }),
		rec("2026-05-10", 0, func(m *app.MeasurementView) { m.WeightKg = num("70.0") }),
	}
	got := app.MergeByDay(list)
	assert.Len(t, got, 1)
	assert.Equal(t, "70.0", got[0].WeightKg.String())
	assert.Equal(t, "170.0", got[0].HeightCm.String())
	assert.Equal(t, "80.0", got[0].WaistCm.String())
	assert.Nil(t, got[0].HipCm)
	assert.Nil(t, got[0].ChestCm)
	assert.Nil(t, got[0].BodyFatPct)
	assert.Equal(t, list[0].ID, got[0].ID)
}

func TestMergeByDayLaterWins(t *testing.T) {
	list := []app.MeasurementView{
		rec("2026-05-10", 30, func(m *app.MeasurementView) { m.WeightKg = num("71.0") }),
		rec("2026-05-10", 20, func(m *app.MeasurementView) { m.WeightKg = num("70.5"); m.HipCm = num("90.0") }),
		rec("2026-05-10", 0, func(m *app.MeasurementView) { m.WeightKg = num("70.0") }),
	}
	got := app.MergeByDay(list)
	assert.Len(t, got, 1)
	assert.Equal(t, "71.0", got[0].WeightKg.String())
	assert.Equal(t, "90.0", got[0].HipCm.String())
}

func TestMergeByDayKeepsDaysAndOrder(t *testing.T) {
	list := []app.MeasurementView{
		rec("2026-05-12", 0, func(m *app.MeasurementView) { m.WeightKg = num("72.0") }),
		rec("2026-05-10", 10, func(m *app.MeasurementView) { m.HeightCm = num("170.0") }),
		rec("2026-05-10", 0, func(m *app.MeasurementView) { m.WeightKg = num("70.0") }),
		rec("2026-05-08", 0, func(m *app.MeasurementView) { m.WeightKg = num("69.0") }),
	}
	got := app.MergeByDay(list)
	assert.Len(t, got, 3)
	assert.Equal(t, []string{"2026-05-12", "2026-05-10", "2026-05-08"}, []string{got[0].MeasuredOn, got[1].MeasuredOn, got[2].MeasuredOn})
	assert.Nil(t, got[0].HeightCm)
	assert.Len(t, list, 4, "input untouched")
	assert.Nil(t, list[2].HeightCm)
}

func TestMergeByDayNotes(t *testing.T) {
	list := []app.MeasurementView{
		rec("2026-05-10", 30, func(m *app.MeasurementView) { m.Notes = "Después de entrenar" }),
		rec("2026-05-10", 20, func(m *app.MeasurementView) { m.Notes = "  " }),
		rec("2026-05-10", 10, func(m *app.MeasurementView) { m.Notes = "En ayunas" }),
		rec("2026-05-10", 0, func(m *app.MeasurementView) { m.Notes = "En ayunas" }),
		rec("2026-05-09", 0, func(m *app.MeasurementView) {}),
	}
	got := app.MergeByDay(list)
	assert.Len(t, got, 2)
	assert.Equal(t, "En ayunas · Después de entrenar", got[0].Notes)
	assert.Equal(t, "", got[1].Notes)
}

func TestMergeByDayEmpty(t *testing.T) {
	assert.Empty(t, app.MergeByDay(nil))
}
