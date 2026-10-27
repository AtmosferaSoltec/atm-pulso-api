package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, handler http.HandlerFunc) *HTTPClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewHTTPClient(server.URL+"/", "atm_test_key", time.Second)
}

func TestPersonResolvesWithTheAPIKey(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/persons/74455518", r.URL.Path)
		assert.Equal(t, "atm_test_key", r.Header.Get("X-API-Key"))
		_, _ = w.Write([]byte(`{"success":true,"data":{"document_number":"74455518","first_names":"JOHN","paternal_surname":"DOE","maternal_surname":"ROE","full_name":"JOHN DOE ROE"},"meta":{"source":"database"}}`))
	})
	person, err := client.Person(context.Background(), "74455518")
	require.NoError(t, err)
	assert.Equal(t, Person{DocumentNumber: "74455518", FirstNames: "JOHN", PaternalSurname: "DOE", MaternalSurname: "ROE"}, person)
}

func TestCompanyResolves(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/companies/20123456789", r.URL.Path)
		_, _ = w.Write([]byte(`{"success":true,"data":{"document_number":"20123456789","legal_name":"ACME S.A.C.","trade_name":null,"address":"AV. UNO 1"}}`))
	})
	company, err := client.Company(context.Background(), "20123456789")
	require.NoError(t, err)
	assert.Equal(t, Company{DocumentNumber: "20123456789", LegalName: "ACME S.A.C.", Address: "AV. UNO 1"}, company)
}

func TestStatusesBecomeTypedErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		want   error
	}{
		"not found":      {http.StatusNotFound, ErrNotFound},
		"unauthorized":   {http.StatusUnauthorized, ErrRejected},
		"forbidden":      {http.StatusForbidden, ErrRejected},
		"rate limited":   {http.StatusTooManyRequests, ErrUnavailable},
		"upstream error": {http.StatusBadGateway, ErrUnavailable},
		"upstream slow":  {http.StatusGatewayTimeout, ErrUnavailable},
		"internal":       {http.StatusInternalServerError, ErrUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
			_, err := client.Person(context.Background(), "74455518")
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestUnreadableAnswerAndUnreachableServiceAreUnavailable(t *testing.T) {
	garbage := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) })
	_, err := garbage.Person(context.Background(), "74455518")
	assert.ErrorIs(t, err, ErrUnavailable)

	down := NewHTTPClient("http://127.0.0.1:1", "key", 200*time.Millisecond)
	_, err = down.Company(context.Background(), "20123456789")
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestSlowServiceTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	client := NewHTTPClient(server.URL, "key", 100*time.Millisecond)
	_, err := client.Person(context.Background(), "74455518")
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestFakeIsStableAndCanMissDocuments(t *testing.T) {
	fake := Fake{}
	first, err := fake.Person(context.Background(), "45678912")
	require.NoError(t, err)
	again, _ := fake.Person(context.Background(), "45678912")
	assert.Equal(t, first, again)
	assert.NotEmpty(t, first.FirstNames)

	_, err = fake.Person(context.Background(), "00000000")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = fake.Company(context.Background(), "00000000000")
	assert.ErrorIs(t, err, ErrNotFound)
	company, err := fake.Company(context.Background(), "20123456789")
	require.NoError(t, err)
	assert.Contains(t, company.LegalName, "S.A.C.")
}
