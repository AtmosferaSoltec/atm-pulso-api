package integration_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/httpserver"
)

var (
	paramColon  = regexp.MustCompile(`:[A-Za-z_]+`)
	paramBraces = regexp.MustCompile(`\{[A-Za-z_]+\}`)
	pathLine    = regexp.MustCompile(`^  (/[^:\s]*):\s*$`)
	methodLine  = regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
)

// documentedRoutes reads "METHOD /path" pairs from the paths section of openapi.yaml.
func documentedRoutes(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../docs/openapi.yaml")
	require.NoError(t, err)
	var routes []string
	path, inPaths := "", false
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
			inPaths = false
		}
		if !inPaths {
			continue
		}
		if m := pathLine.FindStringSubmatch(line); m != nil {
			path = m[1]
		} else if m := methodLine.FindStringSubmatch(line); m != nil && path != "" {
			routes = append(routes, strings.ToUpper(m[1])+" "+httpserver.APIPrefix+paramBraces.ReplaceAllString(path, "{}"))
		}
	}
	sort.Strings(routes)
	return routes
}

// The contract and the code must describe the same routes: a new endpoint without its
// entry in docs/openapi.yaml (or the other way around) fails here.
func TestOpenAPIListsExactlyTheRegisteredRoutes(t *testing.T) {
	h := newHarness(t)
	var registered []string
	for _, route := range h.router.Routes() {
		registered = append(registered, route.Method+" "+paramColon.ReplaceAllString(route.Path, "{}"))
	}
	sort.Strings(registered)
	documented := documentedRoutes(t)
	require.NotEmpty(t, documented)
	assert.Equal(t, registered, documented)
}

func TestResponsesCarrySecurityHeaders(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/health", "/me", "/does-not-exist"} {
		res := h.call(t, "GET", path, "", nil)
		assert.Equal(t, "nosniff", res.header.Get("X-Content-Type-Options"), path)
		assert.Equal(t, "no-store", res.header.Get("Cache-Control"), path)
		assert.Equal(t, "DENY", res.header.Get("X-Frame-Options"), path)
		assert.Equal(t, "no-referrer", res.header.Get("Referrer-Policy"), path)
	}
}
