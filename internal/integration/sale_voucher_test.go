package integration_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fieldCodes lists the "field:code" pairs of a VALIDATION_FAILED answer.
func fieldCodes(r response) []string {
	var out []string
	e, _ := r.body["error"].(map[string]any)
	details, _ := e["details"].([]any)
	for _, d := range details {
		item, _ := d.(map[string]any)
		field, _ := item["field"].(string)
		code, _ := item["code"].(string)
		out = append(out, field+":"+code)
	}
	return out
}

func TestSaleVouchers(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	h.setSales(t, other.GymOne, true)
	admin, reception, otherAdmin := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, other.Admin)
	batido := h.product(t, admin, "Batido", "8.00")
	ana, beto := h.memberID(t, admin, tn.BranchA), h.memberID(t, admin, tn.BranchA)
	hidden := h.extraBranch(t, tn) // GymOne, out of the receptionist's reach

	sale := func(token string, body map[string]any) string {
		t.Helper()
		body["items"] = []any{item(batido, 1)}
		res := h.sell(t, token, body)
		require.Equal(t, 201, res.status, res.body)
		return res.data()["id"].(string)
	}
	yape := sale(reception, map[string]any{"branch_id": tn.BranchA, "method": "yape_plin"})
	cash := sale(reception, map[string]any{"branch_id": tn.BranchA, "method": "cash"})
	far := sale(admin, map[string]any{"branch_id": hidden, "method": "card"})
	open1 := sale(reception, map[string]any{"branch_id": tn.BranchA, "status": "pending", "member_id": ana})
	open2 := sale(reception, map[string]any{"branch_id": tn.BranchA, "status": "pending", "member_id": ana})
	openBeto := sale(admin, map[string]any{"branch_id": tn.BranchA, "status": "pending", "member_id": beto})
	onSale := func(token, id string, n int) response {
		return h.postFile(t, token, "/sales/"+id+"/vouchers", proof(n))
	}

	// No money yet: a pending sale takes no proof; cash needs none; the file must be a real image or PDF.
	res := onSale(reception, open1, 0)
	assert.Equal(t, 409, res.status)
	assert.Equal(t, "VOUCHER_SALE_NOT_PAID", res.errorCode())
	res = onSale(reception, cash, 0)
	assert.Equal(t, 400, res.status)
	assert.Equal(t, []string{"sale:VOUCHER_NOT_FOR_CASH"}, fieldCodes(res))
	res = h.postFile(t, reception, "/sales/"+yape+"/vouchers", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"))
	assert.Equal(t, []string{"voucher:VOUCHER_TYPE_INVALID"}, fieldCodes(res))
	assert.Equal(t, 400, h.postFile(t, reception, "/sales/"+yape+"/vouchers", []byte{}).status)
	assert.Equal(t, 404, onSale(admin, uuid.NewString(), 0).status)
	assert.Equal(t, 404, onSale(admin, "not-a-uuid", 0).status)
	assert.Zero(t, h.vouchers.count(), "a rejected file is never stored")

	// Up to five per sale, a PDF among them; the sixth is refused and not kept.
	first := onSale(reception, yape, 1)
	require.Equal(t, 201, first.status, first.body)
	assert.Equal(t, "image/png", first.data()["content_type"])
	assert.Nil(t, first.data()["storage_key"], "the key never leaves the server")
	for i := 2; i <= 4; i++ {
		require.Equal(t, 201, onSale(reception, yape, i).status)
	}
	require.Equal(t, 201, h.postFile(t, admin, "/sales/"+yape+"/vouchers", append([]byte("%PDF-1.4\n"), []byte("proof-5")...)).status)
	sixth := onSale(reception, yape, 6)
	assert.Equal(t, 409, sixth.status)
	assert.Equal(t, "VOUCHER_LIMIT_REACHED", sixth.errorCode())
	assert.Equal(t, 5, h.vouchers.count())

	// Listing and downloading through the same file endpoint as the payments'.
	list := h.call(t, http.MethodGet, "/sales/"+yape+"/vouchers", reception, nil)
	require.Equal(t, 200, list.status, list.body)
	items, _ := list.body["data"].([]any)
	require.Len(t, items, 5)
	voucher := items[0].(map[string]any)["id"].(string)
	file := h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", reception)
	require.Equal(t, 200, file.Code)
	assert.Equal(t, proof(1), file.Body.Bytes())
	assert.Equal(t, "image/png", file.Header().Get("Content-Type"))
	assert.Equal(t, 401, h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", "").Code)
	pendingList := h.call(t, http.MethodGet, "/sales/"+open2+"/vouchers", reception, nil)
	require.Equal(t, 200, pendingList.status)
	assert.Empty(t, pendingList.body["data"], "a pending sale has none")

	// Reach: the branch of the sale. Reception does not reach the hidden branch; the admin does.
	farVoucher := onSale(admin, far, 1)
	require.Equal(t, 201, farVoucher.status, farVoucher.body)
	farID := farVoucher.data()["id"].(string)
	assert.Equal(t, 404, onSale(reception, far, 2).status)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/sales/"+far+"/vouchers", reception, nil).status)
	assert.Equal(t, 404, h.callRaw(t, http.MethodGet, "/vouchers/"+farID+"/file", reception).Code)
	assert.Equal(t, 200, h.callRaw(t, http.MethodGet, "/vouchers/"+farID+"/file", admin).Code)

	// Another gym sees nothing.
	assert.Equal(t, 404, onSale(otherAdmin, yape, 7).status)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/sales/"+yape+"/vouchers", otherAdmin, nil).status)
	assert.Equal(t, 404, h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", otherAdmin).Code)

	// Collections: the proof of pending sales paid later belongs to the collection, not to the sales.
	collect := func(token string, body map[string]any) string {
		t.Helper()
		res := h.call(t, http.MethodPost, "/sales/collections", token, body)
		require.Equal(t, 201, res.status, res.body)
		return res.data()["id"].(string)
	}
	transfer := collect(reception, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "transfer", "sale_ids": []any{open1}})
	cashCollection := collect(reception, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "cash", "sale_ids": []any{open2}})
	farCollection := collect(admin, map[string]any{"member_id": beto, "branch_id": hidden, "method": "yape_plin"})
	onCollection := func(token, id string, n int) response {
		return h.postFile(t, token, "/sales/collections/"+id+"/vouchers", proof(100+n))
	}
	res = onSale(reception, open1, 1)
	assert.Equal(t, 409, res.status)
	assert.Equal(t, "VOUCHER_SALE_COLLECTED", res.errorCode())
	res = onCollection(reception, cashCollection, 1)
	assert.Equal(t, []string{"collection:VOUCHER_NOT_FOR_CASH"}, fieldCodes(res))
	for i := 1; i <= 2; i++ {
		created := onCollection(reception, transfer, i)
		require.Equal(t, 201, created.status, created.body)
	}
	collectionList := h.call(t, http.MethodGet, "/sales/collections/"+transfer+"/vouchers", reception, nil)
	require.Equal(t, 200, collectionList.status)
	collectionItems := collectionList.body["data"].([]any)
	require.Len(t, collectionItems, 2)
	collectionFile := h.callRaw(t, http.MethodGet, "/vouchers/"+collectionItems[1].(map[string]any)["id"].(string)+"/file", reception)
	require.Equal(t, 200, collectionFile.Code)
	assert.Equal(t, proof(102), collectionFile.Body.Bytes())
	assert.Equal(t, 404, onCollection(reception, farCollection, 1).status, "collected in a branch out of reach")
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/sales/collections/"+farCollection+"/vouchers", reception, nil).status)
	assert.Equal(t, 201, onCollection(admin, farCollection, 1).status)
	assert.Equal(t, 404, onCollection(otherAdmin, transfer, 3).status, "another gym")
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/sales/collections/"+transfer+"/vouchers", otherAdmin, nil).status)
	assert.Equal(t, 404, onCollection(admin, uuid.NewString(), 3).status)
	assert.Equal(t, 404, onCollection(admin, yape, 3).status, "a sale id is not a collection")

	// The sales list says how many proofs back each sale: its own, or its collection's when collected later.
	sales := h.call(t, http.MethodGet, "/sales?page_size=100", admin, nil)
	require.Equal(t, 200, sales.status, sales.body)
	counts := map[string]float64{}
	for _, row := range sales.body["data"].([]any) {
		r := row.(map[string]any)
		counts[r["id"].(string)] = r["voucher_count"].(float64)
	}
	assert.Equal(t, map[string]float64{yape: 5, cash: 0, far: 1, open1: 2, open2: 0, openBeto: 1}, counts)
	history := h.call(t, http.MethodGet, "/members/"+ana+"/sales", reception, nil)
	require.Equal(t, 200, history.status)
	for _, row := range history.body["data"].([]any) {
		r := row.(map[string]any)
		assert.Equal(t, counts[r["id"].(string)], r["voucher_count"], r["id"])
	}

	// Without the sales module nothing of this answers, not even the files of sales' vouchers.
	h.setSales(t, tn.GymOne, false)
	for _, path := range []string{"/sales/" + yape + "/vouchers", "/sales/collections/" + transfer + "/vouchers"} {
		assert.Equal(t, "FEATURE_DISABLED", h.call(t, http.MethodGet, path, admin, nil).errorCode(), path)
		assert.Equal(t, "FEATURE_DISABLED", h.postFile(t, admin, path, proof(9)).errorCode(), path)
	}
	assert.Equal(t, 403, h.callRaw(t, http.MethodGet, "/vouchers/"+voucher+"/file", admin).Code)
}

func TestSaleVoucherLimitHoldsUnderConcurrency(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	admin := h.token(t, tn.Admin)
	batido := h.product(t, admin, "Batido", "8.00")
	res := h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "method": "transfer", "items": []any{item(batido, 1)}})
	require.Equal(t, 201, res.status, res.body)
	id := res.data()["id"].(string)

	var wg sync.WaitGroup
	statuses := make(chan int, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- h.postFile(t, admin, "/sales/"+id+"/vouchers", proof(i)).status
		}()
	}
	wg.Wait()
	close(statuses)
	byStatus := map[int]int{}
	for s := range statuses {
		byStatus[s]++
	}
	assert.Equal(t, map[int]int{201: 5, 409: 3}, byStatus)
	assert.Equal(t, 5, h.vouchers.count(), "the refused files are deleted from the store")
}
