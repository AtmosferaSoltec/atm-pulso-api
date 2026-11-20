package integration_test

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
)

var exportFormats = []struct {
	format, contentType, ext string
}{
	{"csv", "text/csv; charset=utf-8", "csv"},
	{"xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"},
	{"pdf", "application/pdf", "pdf"},
}

// download is one answered download.
type download struct {
	status      int
	contentType string
	disposition string
	body        []byte
}

func (h harness) download(t *testing.T, path, token string) download {
	t.Helper()
	res := h.callRaw(t, http.MethodGet, path, token)
	return download{status: res.Code, contentType: res.Header().Get("Content-Type"), disposition: res.Header().Get("Content-Disposition"), body: res.Body.Bytes()}
}

// table reads a CSV (without its BOM) or the first sheet of an xlsx as rows of text.
func table(t *testing.T, format string, body []byte) [][]string {
	t.Helper()
	if format == "csv" {
		require.True(t, bytes.HasPrefix(body, []byte("\xEF\xBB\xBF")), "UTF-8 BOM so Excel opens it")
		records, err := csv.NewReader(bytes.NewReader(body[3:])).ReadAll()
		require.NoError(t, err)
		return records
	}
	book, err := excelize.OpenReader(bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = book.Close() }()
	rows, err := book.GetRows(book.GetSheetName(0))
	require.NoError(t, err)
	return rows
}

// keepSample writes a PDF to EXPORT_SAMPLE_DIR when it is set, to look at the design.
func keepSample(t *testing.T, name string, body []byte) {
	if dir := os.Getenv("EXPORT_SAMPLE_DIR"); dir != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), body, 0o600))
	}
}

// assertDownloads checks every format of one endpoint: status, media type, attachment name, the PDF signature
// and that the CSV and the Excel carry the same header (and the same number of rows). It returns the CSV rows.
func assertDownloads(t *testing.T, h harness, path, token, base string) [][]string {
	t.Helper()
	today := limaToday().Format("2006-01-02")
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	var csvRows [][]string
	for _, f := range exportFormats {
		d := h.download(t, path+sep+"format="+f.format, token)
		require.Equal(t, http.StatusOK, d.status, "%s %s", f.format, d.body)
		assert.Equal(t, f.contentType, d.contentType, f.format)
		assert.Equal(t, fmt.Sprintf(`attachment; filename="%s-%s.%s"`, base, today, f.ext), d.disposition, f.format)
		switch f.format {
		case "csv":
			csvRows = table(t, "csv", d.body)
		case "xlsx":
			rows := table(t, "xlsx", d.body)
			require.NotEmpty(t, rows)
			assert.Equal(t, csvRows[0], rows[0], "CSV and Excel have the same columns, headers and order")
			assert.GreaterOrEqual(t, len(rows), len(csvRows), "the Excel has every row (and maybe a total row)")
		case "pdf":
			assert.True(t, bytes.HasPrefix(d.body, []byte("%PDF-")), "a PDF")
			keepSample(t, base+".pdf", d.body)
		}
	}
	return csvRows
}

func TestPaymentDownloadsInEveryFormat(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, reception, adminTwo := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, tn.AdminTwo)

	for i, name := range []string{"José Ñañez Peña", "María Fernández", "=HYPERLINK(1) Pérez"} {
		body := enrollBody(tn.BranchA, uniqueDocument())
		body["full_name"], body["amount"] = name, fmt.Sprintf("%d.50", 100+i*35)
		require.Equal(t, 201, h.enroll(t, admin, body).status)
	}
	require.Equal(t, 201, h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "cash"}).status)

	rows := assertDownloads(t, h, "/payments/export?branch_id="+tn.BranchA.String(), admin, "pagos")
	require.Len(t, rows, 5, "header and four payments")
	assert.Equal(t, []string{"Boleta", "Miembro", "Documento", "Plan", "Importe", "Medio de pago", "Fecha"}, rows[0])

	// The historical URL (no format) is still the CSV.
	legacy := h.download(t, "/payments/export", admin)
	require.Equal(t, 200, legacy.status)
	assert.Equal(t, "text/csv; charset=utf-8", legacy.contentType)

	// Excel: amounts are numbers, dates are dates, there is a bold total row.
	xlsx := h.download(t, "/payments/export?format=xlsx&method=cash", admin)
	require.Equal(t, 200, xlsx.status)
	book, err := excelize.OpenReader(bytes.NewReader(xlsx.body))
	require.NoError(t, err)
	sheet := book.GetSheetName(0)
	assert.Equal(t, "Pagos", sheet)
	raw, err := book.GetCellValue(sheet, "G2", excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	assert.NotContains(t, raw, "-", "the date is a serial number, not text")
	shown, err := book.GetCellValue(sheet, "G2")
	require.NoError(t, err)
	assert.Equal(t, limaToday().Format("02/01/2006"), shown)

	// Finance stays admin-only, whatever the format; filters are validated like the list.
	for _, f := range exportFormats {
		res := h.call(t, http.MethodGet, "/payments/export?format="+f.format, reception, nil)
		assert.Equal(t, 403, res.status, f.format)
		assert.Equal(t, 403, h.call(t, http.MethodGet, "/payments/export?format="+f.format, h.superToken(t), nil).status)
		assert.Equal(t, 401, h.call(t, http.MethodGet, "/payments/export?format="+f.format, "", nil).status)
	}
	bad := h.call(t, http.MethodGet, "/payments/export?format=docx", admin, nil)
	assert.Equal(t, 400, bad.status)
	assert.Equal(t, "VALIDATION_FAILED", bad.errorCode())
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/payments/export?format=pdf&method=nope", admin, nil).status)

	// Another gym's admin downloads only their own (empty) payments.
	other := table(t, "csv", h.download(t, "/payments/export?format=csv", adminTwo).body)
	assert.Len(t, other, 1)
}

// pngServer serves a small logo, like the signed link of atm-storage-api.
func pngServer(t *testing.T) *httptest.Server {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 240, 80))
	for x := range 240 {
		for y := range 80 {
			if (x-40)*(x-40)+(y-40)*(y-40) < 34*34 || (x > 90 && y > 28 && y < 52) {
				img.Set(x, y, color.NRGBA{0xe1, 0x5a, 0x2b, 0xff})
			}
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(server.Close)
	return server
}

func TestPDFLetterheadUsesTheLogoAndNeverFailsWithoutIt(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, adminTwo := h.token(t, tn.Admin), h.token(t, tn.AdminTwo)
	require.Equal(t, 201, h.enroll(t, admin, enrollBody(tn.BranchA, uniqueDocument())).status)

	server := pngServer(t)
	logo := uuid.NewString()
	h.auth.SetTenant(authapi.Tenant{ID: tn.TenantOne.String(), DisplayName: "Gym Uno", LogoFileID: &logo})
	h.storage.SetLink(logo, server.URL+"/logo.png?sig=abc", time.Now().Add(time.Hour))
	_, err := h.pool.Exec(t.Context(), `UPDATE gyms SET brand_color = '#e15a2b', display_name = 'Gimnasio Fuerza Peña' WHERE id = $1`, tn.GymOne)
	require.NoError(t, err)

	withLogo := h.download(t, "/payments/export?format=pdf", admin)
	require.Equal(t, 200, withLogo.status)
	assert.Contains(t, string(withLogo.body), "/Subtype /Image", "the logo is drawn")
	keepSample(t, "pagos-con-logo.pdf", withLogo.body)

	// A broken link (the file is gone) or no logo at all: the PDF is still made, with a monogram.
	broken := uuid.NewString()
	h.auth.SetTenant(authapi.Tenant{ID: tn.TenantTwo.String(), DisplayName: "Gym Dos", LogoFileID: &broken})
	h.storage.SetLink(broken, server.URL+"/missing?sig=x", time.Now().Add(time.Hour))
	server.Config.Handler = http.NotFoundHandler()
	without := h.download(t, "/payments/export?format=pdf", adminTwo)
	require.Equal(t, 200, without.status)
	assert.NotContains(t, string(without.body), "/Subtype /Image")
}

func TestMemberDownloadsFollowTheListRules(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, reception, adminTwo := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, tn.AdminTwo)
	for _, name := range []string{"Ángela Muñoz", "Íñigo Peña", "Zoe Prueba"} {
		body := enrollBody(tn.BranchA, uniqueDocument())
		body["full_name"] = name
		require.Equal(t, 201, h.enroll(t, admin, body).status)
	}
	future := enrollBody(tn.BranchB, uniqueDocument())
	future["full_name"], future["starts_on"] = "Futuro Socio", iso(limaToday().AddDate(0, 0, 10))
	require.Equal(t, 201, h.enroll(t, admin, future).status)

	// Admin and reception may list the directory, so both may download it.
	rows := assertDownloads(t, h, "/members/export", admin, "miembros")
	require.Len(t, rows, 5)
	assert.Equal(t, []string{"Tipo de documento", "Documento", "Nombre", "Teléfono", "Sede", "Estado", "Plan", "Inicio", "Fin",
		"Días restantes", "Última boleta"}, rows[0])
	assertDownloads(t, h, "/members/export?status=active", reception, "miembros")

	// The filters of the list apply: status, search and home branch.
	scheduled := table(t, "csv", h.download(t, "/members/export?status=scheduled", reception).body)
	require.Len(t, scheduled, 2)
	assert.Equal(t, "Futuro Socio", scheduled[1][2])
	assert.Equal(t, "Por iniciar", scheduled[1][5])
	search := table(t, "csv", h.download(t, "/members/export?q=peña", admin).body)
	require.Len(t, search, 2)
	assert.Equal(t, "Íñigo Peña", search[1][2])
	branch := table(t, "csv", h.download(t, "/members/export?home_branch_id="+tn.BranchB.String(), admin).body)
	assert.Len(t, branch, 2)

	// Isolation: another gym gets none of these members.
	assert.Len(t, table(t, "csv", h.download(t, "/members/export", adminTwo).body), 1)

	assert.Equal(t, 400, h.call(t, http.MethodGet, "/members/export?status=nope", admin, nil).status)
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/members/export?format=doc", admin, nil).status)
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/members/export?home_branch_id="+tn.BranchC.String(), admin, nil).status, "a branch of another gym")
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/members/export", h.superToken(t), nil).status)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/members/export", "", nil).status)
}

func TestSalesDownloadsAreAdminOnlyAndBehindTheModule(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)

	off := h.call(t, http.MethodGet, "/sales/export?format=xlsx", admin, nil)
	assert.Equal(t, 403, off.status)
	assert.Equal(t, "FEATURE_DISABLED", off.errorCode())

	h.setSales(t, tn.GymOne, true)
	water, bar := h.product(t, admin, "Agua sin gas", "2.50"), h.product(t, admin, "Barra de proteína", "8.00")
	member := h.memberID(t, admin, tn.BranchA)
	require.Equal(t, 201, h.sell(t, reception, map[string]any{"branch_id": tn.BranchA, "method": "yape_plin", "items": []any{item(water, 2), item(bar, 1)}}).status)
	require.Equal(t, 201, h.sell(t, reception, map[string]any{"branch_id": tn.BranchB, "member_id": member, "status": "pending", "items": []any{item(bar, 3)}}).status)

	rows := assertDownloads(t, h, "/sales/export", admin, "ventas")
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"Fecha", "Hora", "Sede", "Miembro", "Documento", "Productos", "Unidades", "Total", "Estado", "Medio de pago", "Vendido por"}, rows[0])
	var paid []string
	for _, r := range rows[1:] {
		if r[8] == "Pagado" {
			paid = r
		}
	}
	require.NotNil(t, paid)
	assert.Equal(t, "2 × Agua sin gas, 1 × Barra de proteína", paid[5])
	assert.Equal(t, "13.00", paid[7])
	assert.Equal(t, "Yape/Plin", paid[9])

	byBranch := table(t, "csv", h.download(t, "/sales/export?branch_id="+tn.BranchB.String(), admin).body)
	require.Len(t, byBranch, 2)
	assert.Equal(t, "Pendiente", byBranch[1][8])

	for _, f := range exportFormats {
		assert.Equal(t, 403, h.call(t, http.MethodGet, "/sales/export?format="+f.format, reception, nil).status, "reception has no sales report")
	}
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/sales/export?from=2026-13-01", admin, nil).status)
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/sales/export?format=txt", admin, nil).status)
}

func TestMeasurementsPDFIsGuardedAndAuditedLikeTheReads(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, reception, adminTwo := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, tn.AdminTwo)
	body := enrollBody(tn.BranchA, uniqueDocument())
	body["full_name"] = "José Ñañez Peña"
	res := h.enroll(t, admin, body)
	require.Equal(t, 201, res.status)
	member := memberIDOf(res)
	path := "/members/" + member + "/measurements/pdf"

	// Module off: refused, nothing logged.
	off := h.call(t, http.MethodGet, path, admin, nil)
	assert.Equal(t, 403, off.status)
	assert.Equal(t, "FEATURE_DISABLED", off.errorCode())
	assert.Empty(t, h.auditRows(t, member))

	h.setBodyTracking(t, tn.GymOne, true)
	// An empty history still makes a PDF (and the read is logged).
	empty := h.download(t, path, admin)
	require.Equal(t, 200, empty.status, string(empty.body))
	keepSample(t, "medidas-vacio.pdf", empty.body)

	weights := []struct {
		day                       int
		weight, fat, waist, chest string
		notes                     string
	}{
		{150, "84.6", "24.1", "96.0", "104.0", "Inicio del plan. Objetivo: bajar 8 kg sin perder fuerza."},
		{120, "83.1", "23.4", "94.5", "103.5", ""},
		{90, "81.9", "22.6", "92.0", "103.0", "Subió la carga en sentadilla; dieta con más proteína."},
		{60, "80.4", "21.5", "90.5", "102.0", ""},
		{30, "79.8", "20.9", "89.0", "101.5", "Molestia leve en la rodilla izquierda: bajar impacto dos semanas."},
		{0, "78.2", "20.1", "87.5", "101.0", "Muy buen avance. Mantener rutina y revisar en un mes."},
	}
	for _, w := range weights {
		m := map[string]any{"measured_on": daysAgo(w.day), "weight_kg": w.weight, "body_fat_pct": w.fat, "waist_cm": w.waist, "chest_cm": w.chest, "notes": w.notes}
		if w.day == 150 {
			m["height_cm"], m["hip_cm"] = "176.0", "101.0"
		}
		require.Equal(t, 201, h.call(t, http.MethodPost, "/members/"+member+"/measurements", reception, m).status)
	}
	before := len(h.auditRows(t, member))

	pdf := h.download(t, path, admin)
	require.Equal(t, 200, pdf.status, string(pdf.body))
	assert.Equal(t, "application/pdf", pdf.contentType)
	assert.Equal(t, `attachment; filename="medidas-jose-nanez-pena-`+today()+`.pdf"`, pdf.disposition)
	assert.True(t, bytes.HasPrefix(pdf.body, []byte("%PDF-")))
	keepSample(t, "medidas.pdf", pdf.body)
	rows := h.auditRows(t, member)
	require.Len(t, rows, before+1, "the PDF is one audited read")
	assert.Equal(t, "read:measurements:false", rows[len(rows)-1])

	// Reception with access to the member's branch may download it too (and it is logged).
	require.Equal(t, 200, h.download(t, path, reception).status)
	assert.Len(t, h.auditRows(t, member), before+2)

	// Refusals are the same as the reads and leave no trace.
	far := h.bodyMember(t, admin, h.extraBranch(t, tn))
	denied := h.call(t, http.MethodGet, "/members/"+far+"/measurements/pdf", reception, nil)
	assert.Equal(t, 403, denied.status)
	assert.Equal(t, "BRANCH_NOT_ACCESSIBLE", denied.errorCode())
	assert.Empty(t, h.auditRows(t, far))
	h.setBodyTracking(t, tn.GymTwo, true)
	foreign := h.call(t, http.MethodGet, path, adminTwo, nil)
	assert.Equal(t, 404, foreign.status)
	assert.Equal(t, "MEMBER_NOT_FOUND", foreign.errorCode())
	assert.Equal(t, 403, h.call(t, http.MethodGet, path, h.superToken(t), nil).status)
	assert.Equal(t, 401, h.call(t, http.MethodGet, path, "", nil).status)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/members/not-an-id/measurements/pdf", admin, nil).status)
	assert.Len(t, h.auditRows(t, member), before+2)
}
