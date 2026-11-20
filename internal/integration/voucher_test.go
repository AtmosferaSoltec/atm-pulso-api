package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/httpserver"
)

type stored struct {
	contentType string
	data        []byte
}

// voucherRetentionDays is the harness's VOUCHER_RETENTION_DAYS; tests age a voucher by moving its created_at back.
const voucherRetentionDays = 90

// memoryVouchers is the object store in memory: tests never reach Cloudflare.
type memoryVouchers struct {
	mu      sync.Mutex
	objects map[string]stored
}

func newMemoryVouchers() *memoryVouchers { return &memoryVouchers{objects: map[string]stored{}} }

func (m *memoryVouchers) Put(_ context.Context, key, contentType string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = stored{contentType, data}
	return nil
}

func (m *memoryVouchers) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memoryVouchers) Get(_ context.Context, key string) (app.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	object, ok := m.objects[key]
	if !ok {
		return app.File{}, domain.ErrVoucherNotFound
	}
	return app.File{Body: io.NopCloser(bytes.NewReader(object.data)), ContentType: object.contentType, Size: int64(len(object.data))}, nil
}

func (m *memoryVouchers) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}

// postVoucher sends a file as the raw body, like the web does.
func (h harness) postVoucher(t *testing.T, token, receipt string, body []byte) response {
	t.Helper()
	return h.postFile(t, token, "/receipts/"+receipt+"/vouchers", body)
}

// postFile sends a file as the raw body to path (under the API prefix).
func (h harness) postFile(t *testing.T, token, path string, body []byte) response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, httpserver.APIPrefix+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	out := response{status: rec.Code, header: rec.Header()}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

// proof is a distinct PNG per n.
func proof(n int) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), []byte(fmt.Sprintf("proof-%d", n))...)
}

func (h harness) sellWith(t *testing.T, token string, branch uuid.UUID, method string) string {
	t.Helper()
	body := enrollBody(branch, uniqueDocument())
	body["method"] = method
	res := h.enroll(t, token, body)
	require.Equal(t, http.StatusCreated, res.status, res.body)
	return res.data()["receipt"].(map[string]any)["id"].(string)
}

func TestPaymentVouchers(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin, adminTwo := h.token(t, tn.Admin), h.token(t, tn.AdminTwo)
	reception := h.token(t, tn.Receptionist)
	foreignAdmin := h.token(t, other.Admin)

	transfer := h.sellWith(t, reception, tn.BranchA, "transfer")
	cash := h.sellWith(t, reception, tn.BranchA, "cash")
	elsewhere := h.sellWith(t, adminTwo, tn.BranchC, "yape_plin") // GymTwo: out of the admin's and the receptionist's reach

	// Cash needs none; the file must be a real image or PDF of a sensible size.
	res := h.postVoucher(t, reception, cash, proof(0))
	assert.Equal(t, 400, res.status)
	assert.Equal(t, "VALIDATION_FAILED", res.errorCode())
	assert.Equal(t, 400, h.postVoucher(t, reception, transfer, []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")).status)
	assert.Equal(t, 400, h.postVoucher(t, reception, transfer, []byte{}).status)
	assert.Equal(t, 400, h.postVoucher(t, reception, transfer, append([]byte("%PDF-1.4"), make([]byte, domain.MaxBytes-7)...)).status)
	assert.Equal(t, 413, h.postVoucher(t, reception, transfer, make([]byte, domain.MaxBytes+1024)).status, "far over the limit the body is cut off")
	assert.Zero(t, h.vouchers.count(), "a rejected file is never stored")

	// Exactly one per payment; a second one (a PDF here) is refused and not kept.
	first := h.postVoucher(t, reception, transfer, proof(1))
	require.Equal(t, 201, first.status, first.body)
	assert.Equal(t, "image/png", first.data()["content_type"])
	assert.Nil(t, first.data()["storage_key"], "the key never leaves the server")
	assertExpiry(t, first.data(), false)
	second := h.postVoucher(t, admin, transfer, append([]byte("%PDF-1.4\n"), []byte("proof-2")...))
	assert.Equal(t, 409, second.status)
	assert.Equal(t, "VOUCHER_LIMIT_REACHED", second.errorCode())
	assert.Equal(t, 1, h.vouchers.count())

	// Listing and downloading, by everyone who reaches the branch; nobody else sees them.
	list := h.call(t, http.MethodGet, "/receipts/"+transfer+"/vouchers", admin, nil)
	require.Equal(t, 200, list.status)
	items, _ := list.body["data"].([]any)
	require.Len(t, items, 1)
	assertExpiry(t, items[0].(map[string]any), false)
	voucher := items[0].(map[string]any)["id"].(string)
	file := h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", admin)
	require.Equal(t, 200, file.Code)
	assert.Equal(t, proof(1), file.Body.Bytes())
	assert.Equal(t, "image/png", file.Header().Get("Content-Type"))
	assert.Contains(t, file.Header().Get("Cache-Control"), "private")
	assert.Equal(t, 401, h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", "").Code, "never without a session")

	assert.Equal(t, 404, h.call(t, http.MethodGet, "/receipts/"+transfer+"/vouchers", foreignAdmin, nil).status, "another client")
	assert.Equal(t, 404, h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", foreignAdmin).Code)
	assert.Equal(t, 404, h.postVoucher(t, foreignAdmin, transfer, proof(7)).status)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/receipts/"+elsewhere+"/vouchers", reception, nil).status, "a branch out of reach")
	assert.Equal(t, 404, h.postVoucher(t, admin, elsewhere, proof(8)).status)
	assert.Equal(t, 201, h.postVoucher(t, adminTwo, elsewhere, proof(9)).status)

	// The payments report says how many each payment has.
	today := iso(limaToday())
	report := h.call(t, http.MethodGet, "/payments?from="+today+"&to="+today+"&branch_id="+tn.BranchA.String(), admin, nil)
	require.Equal(t, 200, report.status, report.body)
	counts := map[string]float64{}
	for _, item := range report.body["data"].([]any) {
		row := item.(map[string]any)
		counts[row["receipt"].(map[string]any)["id"].(string)] = row["voucher_count"].(float64)
	}
	assert.Equal(t, map[string]float64{transfer: 1, cash: 0}, counts)
}

// assertExpiry checks the retention fields of a voucher: expires_at is created_at plus the retention.
func assertExpiry(t *testing.T, voucher map[string]any, expired bool) {
	t.Helper()
	created, err := time.Parse(time.RFC3339Nano, voucher["created_at"].(string))
	require.NoError(t, err)
	expires, err := time.Parse(time.RFC3339Nano, voucher["expires_at"].(string))
	require.NoError(t, err)
	assert.Equal(t, created.AddDate(0, 0, voucherRetentionDays), expires)
	assert.Equal(t, expired, voucher["expired"])
}

// age moves a voucher's created_at back by days, as if it had been uploaded then.
func (h harness) age(t *testing.T, table, voucher string, days int) {
	t.Helper()
	tag, err := h.pool.Exec(t.Context(), `UPDATE `+table+` SET created_at = created_at - make_interval(days => $2) WHERE id = $1`, voucher, days)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}

func (h harness) purgedAt(t *testing.T, table, voucher string) *time.Time {
	t.Helper()
	var at *time.Time
	require.NoError(t, h.pool.QueryRow(t.Context(), `SELECT purged_at FROM `+table+` WHERE id = $1`, voucher).Scan(&at))
	return at
}

func TestVouchersExpireAndArePurged(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	admin := h.token(t, tn.Admin)
	batido := h.product(t, admin, "Batido", "8.00")
	ana := h.memberID(t, admin, tn.BranchA)

	upload := func(path string, n int) string {
		t.Helper()
		res := h.postFile(t, admin, path, proof(n))
		require.Equal(t, 201, res.status, res.body)
		return res.data()["id"].(string)
	}
	oldReceipt, freshReceipt := h.sellWith(t, admin, tn.BranchA, "transfer"), h.sellWith(t, admin, tn.BranchA, "card")
	sold := h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "method": "yape_plin", "items": []any{item(batido, 1)}})
	require.Equal(t, 201, sold.status, sold.body)
	sale := sold.data()["id"].(string)
	pending := h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "status": "pending", "member_id": ana, "items": []any{item(batido, 1)}})
	require.Equal(t, 201, pending.status, pending.body)
	collected := h.call(t, http.MethodPost, "/sales/collections", admin, map[string]any{"member_id": ana, "branch_id": tn.BranchA,
		"method": "transfer", "sale_ids": []any{pending.data()["id"]}})
	require.Equal(t, 201, collected.status, collected.body)
	collection := collected.data()["id"].(string)

	onReceipt := upload("/receipts/"+oldReceipt+"/vouchers", 1)
	onFresh := upload("/receipts/"+freshReceipt+"/vouchers", 2)
	onSale := upload("/sales/"+sale+"/vouchers", 3)
	onCollection := upload("/sales/collections/"+collection+"/vouchers", 4)
	// Three are uploaded "91 days ago"; one is still young (89 days).
	h.age(t, "payment_vouchers", onReceipt, voucherRetentionDays+1)
	h.age(t, "sale_vouchers", onSale, voucherRetentionDays+1)
	h.age(t, "sale_vouchers", onCollection, voucherRetentionDays+1)
	h.age(t, "payment_vouchers", onFresh, voucherRetentionDays-1)

	listed := func(path string) map[string]any {
		t.Helper()
		res := h.call(t, http.MethodGet, path, admin, nil)
		require.Equal(t, 200, res.status, res.body)
		items := res.body["data"].([]any)
		require.Len(t, items, 1)
		return items[0].(map[string]any)
	}
	expiredFile := func(voucher string) {
		t.Helper()
		res := h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", admin)
		assert.Equal(t, http.StatusGone, res.Code, voucher)
		var body map[string]any
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
		assert.Equal(t, "VOUCHER_EXPIRED", body["error"].(map[string]any)["code"])
	}

	// Past the retention a voucher is expired even before the purge removes its file.
	check := func() {
		t.Helper()
		assertExpiry(t, listed("/receipts/"+oldReceipt+"/vouchers"), true)
		assertExpiry(t, listed("/sales/"+sale+"/vouchers"), true)
		assertExpiry(t, listed("/sales/collections/"+collection+"/vouchers"), true)
		assertExpiry(t, listed("/receipts/"+freshReceipt+"/vouchers"), false)
		for _, voucher := range []string{onReceipt, onSale, onCollection} {
			expiredFile(voucher)
		}
		fresh := h.callRaw(t, http.MethodGet, "/vouchers/"+onFresh+"/file", admin)
		require.Equal(t, 200, fresh.Code)
		assert.Equal(t, proof(2), fresh.Body.Bytes())
	}
	check()
	assert.Equal(t, 4, h.vouchers.count(), "nothing deleted before the purge")

	// The purge deletes the three files, stamps their rows and keeps them; the young one is untouched.
	_, err := h.voucherModule.PurgeExpired(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, h.vouchers.count(), "only the young file is left")
	assert.NotNil(t, h.purgedAt(t, "payment_vouchers", onReceipt))
	assert.NotNil(t, h.purgedAt(t, "sale_vouchers", onSale))
	assert.NotNil(t, h.purgedAt(t, "sale_vouchers", onCollection))
	assert.Nil(t, h.purgedAt(t, "payment_vouchers", onFresh))
	check()

	// Counts still count the rows, and the one-per-owner limit still holds for an expired voucher.
	today := iso(limaToday())
	report := h.call(t, http.MethodGet, "/payments?from="+today+"&to="+today+"&branch_id="+tn.BranchA.String(), admin, nil)
	require.Equal(t, 200, report.status, report.body)
	counts := map[string]any{}
	for _, item := range report.body["data"].([]any) {
		row := item.(map[string]any)
		counts[row["receipt"].(map[string]any)["id"].(string)] = row["voucher_count"]
	}
	assert.Equal(t, float64(1), counts[oldReceipt], "a purged voucher still counts")
	assert.Equal(t, float64(1), counts[freshReceipt])
	assert.Equal(t, "VOUCHER_LIMIT_REACHED", h.postVoucher(t, admin, oldReceipt, proof(5)).errorCode())

	// A second run finds nothing left of these.
	before := h.purgedAt(t, "payment_vouchers", onReceipt)
	_, err = h.voucherModule.PurgeExpired(t.Context())
	require.NoError(t, err)
	assert.Equal(t, before, h.purgedAt(t, "payment_vouchers", onReceipt))
	assert.Equal(t, 1, h.vouchers.count())
}
