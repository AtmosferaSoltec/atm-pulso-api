package integration_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Name searches ignore case and Spanish accents in every list that has one.
func TestNameSearchIgnoresAccentsAndCase(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	today := limaToday()
	for _, name := range []string{"José Ramírez Ñique", "MARIA RAMIREZ", "Pedro Gómez"} {
		document := h.memberWith(t, admin, tn.BranchA, today, 1, map[string]any{"full_name": name})
		require.Equal(t, 200, h.check(t, admin, tn.BranchA, document).status)
	}

	total := func(path, q string) float64 {
		t.Helper()
		res := h.call(t, http.MethodGet, path+"?q="+url.QueryEscape(q), admin, nil)
		require.Equal(t, 200, res.status, res.body)
		if meta, ok := res.body["meta"].(map[string]any); ok && meta["total"] != nil {
			return meta["total"].(float64)
		}
		return float64(len(res.body["data"].([]any)))
	}
	for _, path := range []string{"/members", "/access/events", "/payments"} {
		for q, want := range map[string]float64{"ramirez": 2, "Ramírez": 2, "RAMÍREZ": 2, "jose": 1, "JOSÉ": 1, "ñique": 1, "nique": 1, "gomez": 1, "Gómez": 1, "ramos": 0} {
			assert.Equal(t, want, total(path, q), "%s?q=%s", path, q)
		}
	}
}
