package gymbrand

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encoded(t *testing.T, w, h int, asJPEG bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, h/2, color.RGBA{200, 40, 40, 255})
	}
	var buf bytes.Buffer
	if asJPEG {
		require.NoError(t, jpeg.Encode(&buf, img, nil))
	} else {
		require.NoError(t, png.Encode(&buf, img))
	}
	return buf.Bytes()
}

func TestNormalizeGivesAPNGNoLargerThanTheLimit(t *testing.T) {
	out, err := Normalize(encoded(t, 1800, 600, true))
	require.NoError(t, err)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, "png", format)
	assert.Equal(t, 600, cfg.Width)
	assert.Equal(t, 200, cfg.Height)

	small, err := Normalize(encoded(t, 120, 40, false))
	require.NoError(t, err)
	cfg, _, err = image.DecodeConfig(bytes.NewReader(small))
	require.NoError(t, err)
	assert.Equal(t, 120, cfg.Width)

	_, err = Normalize([]byte("<svg/>"))
	assert.Error(t, err)
}

func TestLogoIsFetchedOnceAndFailuresGiveNoLogo(t *testing.T) {
	logo := encoded(t, 200, 80, false)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write(logo)
		case "/huge":
			_, _ = w.Write(make([]byte, MaxLogoBytes+10))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	r := New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	first := r.logo(ctx, server.URL+"/ok")
	require.NotEmpty(t, first)
	assert.Equal(t, first, r.logo(ctx, server.URL+"/ok"))
	assert.Equal(t, int32(1), hits.Load(), "a signed link is downloaded once")

	assert.Nil(t, r.logo(ctx, server.URL+"/missing"))
	assert.Nil(t, r.logo(ctx, server.URL+"/huge"))
	assert.Nil(t, r.logo(ctx, "http://127.0.0.1:1/unreachable"))
	assert.Nil(t, r.logo(ctx, "::not a url"))
}
