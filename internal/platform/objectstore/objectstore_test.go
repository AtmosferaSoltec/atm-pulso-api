package objectstore_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/objectstore"
)

// fakeS3 is the little of the S3 protocol the client uses, path-style: PUT, GET and DELETE of an object.
func fakeS3(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	objects := map[string][]byte{}
	types := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !strings.Contains(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			http.Error(w, "unsigned", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			objects[r.URL.Path], types[r.URL.Path] = body, r.Header.Get("Content-Type")
			w.Header().Set("ETag", `"x"`)
		case http.MethodGet:
			body, ok := objects[r.URL.Path]
			if !ok {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
				return
			}
			w.Header().Set("Content-Type", types[r.URL.Path])
			_, _ = w.Write(body)
		case http.MethodDelete:
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestClientStoresReadsAndDeletes(t *testing.T) {
	server := fakeS3(t)
	client := objectstore.New(objectstore.Config{Endpoint: server.URL, Bucket: "pulso", AccessKeyID: "key", SecretAccessKey: "secret"})
	ctx := t.Context()

	require.NoError(t, client.Put(ctx, "logos/a/b.png", "image/png", []byte("png-bytes")))
	object, err := client.Get(ctx, "logos/a/b.png")
	require.NoError(t, err)
	defer func() { _ = object.Body.Close() }()
	body, _ := io.ReadAll(object.Body)
	assert.Equal(t, "png-bytes", string(body))
	assert.Equal(t, "image/png", object.ContentType)
	assert.EqualValues(t, len("png-bytes"), object.Size)

	require.NoError(t, client.Delete(ctx, "logos/a/b.png"))
	_, err = client.Get(ctx, "logos/a/b.png")
	assert.ErrorIs(t, err, objectstore.ErrNotFound)
}
