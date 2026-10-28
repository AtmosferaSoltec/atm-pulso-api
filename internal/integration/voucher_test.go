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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/httpserver"
)

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

// post sends a file as the raw body, like the web does.
func (h harness) postVoucher(t *testing.T, token, receipt string, body []byte) response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, httpserver.APIPrefix+"/receipts/"+receipt+"/vouchers", bytes.NewReader(body))
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
	owner, admin := h.token(t, tn.Owner, demoPassword), h.token(t, tn.Admin, demoPassword)
	reception := h.token(t, tn.Receptionist, demoPassword)
	super := h.token(t, h.newSuperadmin(t), demoPassword)
	foreignOwner := h.token(t, other.Owner, demoPassword)

	transfer := h.sellWith(t, reception, tn.BranchA, "transfer")
	cash := h.sellWith(t, reception, tn.BranchA, "cash")
	elsewhere := h.sellWith(t, owner, tn.BranchC, "yape_plin") // GymTwo: out of the admin's and the receptionist's reach

	// Cash needs none; the file must be a real image or PDF of a sensible size.
	res := h.postVoucher(t, reception, cash, proof(0))
	assert.Equal(t, 400, res.status)
	assert.Equal(t, "VALIDATION_FAILED", res.errorCode())
	assert.Equal(t, 400, h.postVoucher(t, reception, transfer, []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")).status)
	assert.Equal(t, 400, h.postVoucher(t, reception, transfer, []byte{}).status)
	assert.Equal(t, 400, h.postVoucher(t, reception, transfer, append([]byte("%PDF-1.4"), make([]byte, domain.MaxBytes-7)...)).status)
	assert.Equal(t, 413, h.postVoucher(t, reception, transfer, make([]byte, domain.MaxBytes+1024)).status, "far over the limit the body is cut off")
	assert.Zero(t, h.vouchers.count(), "a rejected file is never stored")

	// Up to five, a PDF among them; the sixth is refused and not kept.
	first := h.postVoucher(t, reception, transfer, proof(1))
	require.Equal(t, 201, first.status, first.body)
	assert.Equal(t, "image/png", first.data()["content_type"])
	assert.Nil(t, first.data()["storage_key"], "the key never leaves the server")
	for i := 2; i <= 4; i++ {
		require.Equal(t, 201, h.postVoucher(t, reception, transfer, proof(i)).status)
	}
	require.Equal(t, 201, h.postVoucher(t, admin, transfer, append([]byte("%PDF-1.4\n"), []byte("proof-5")...)).status)
	sixth := h.postVoucher(t, reception, transfer, proof(6))
	assert.Equal(t, 409, sixth.status)
	assert.Equal(t, "VOUCHER_LIMIT_REACHED", sixth.errorCode())
	assert.Equal(t, 5, h.vouchers.count())

	// Listing and downloading, by everyone who reaches the branch; nobody else sees them.
	list := h.call(t, http.MethodGet, "/receipts/"+transfer+"/vouchers", owner, nil)
	require.Equal(t, 200, list.status)
	items, _ := list.body["data"].([]any)
	require.Len(t, items, 5)
	voucher := items[0].(map[string]any)["id"].(string)
	file := h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", admin)
	require.Equal(t, 200, file.Code)
	assert.Equal(t, proof(1), file.Body.Bytes())
	assert.Equal(t, "image/png", file.Header().Get("Content-Type"))
	assert.Contains(t, file.Header().Get("Cache-Control"), "private")
	assert.Equal(t, 401, h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", "").Code, "never without a session")

	assert.Equal(t, 404, h.call(t, http.MethodGet, "/receipts/"+transfer+"/vouchers", foreignOwner, nil).status, "another client")
	assert.Equal(t, 404, h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", foreignOwner).Code)
	assert.Equal(t, 404, h.postVoucher(t, foreignOwner, transfer, proof(7)).status)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/receipts/"+elsewhere+"/vouchers", reception, nil).status, "a branch out of reach")
	assert.Equal(t, 404, h.postVoucher(t, admin, elsewhere, proof(8)).status)
	assert.Equal(t, 201, h.postVoucher(t, owner, elsewhere, proof(9)).status)
	assert.Equal(t, 403, h.postVoucher(t, super, transfer, proof(10)).status, "the superadmin only consults")

	// The payments report says how many each payment has.
	today := iso(limaToday())
	report := h.call(t, http.MethodGet, "/payments?from="+today+"&to="+today+"&branch_id="+tn.BranchA.String(), owner, nil)
	require.Equal(t, 200, report.status, report.body)
	counts := map[string]float64{}
	for _, item := range report.body["data"].([]any) {
		row := item.(map[string]any)
		counts[row["receipt"].(map[string]any)["id"].(string)] = row["voucher_count"].(float64)
	}
	assert.Equal(t, map[string]float64{transfer: 5, cash: 0}, counts)
}
