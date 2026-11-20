// Package gymbrand gives the letterhead of a gym for the downloads: its name and colour (table gyms, owned by
// the accounts module, read with one SELECT) and the bytes of its logo, fetched from the temporary signed link
// the logo resolver hands out. It writes nothing; modules never import each other.
//
// The logo is best effort: no resolver, no logo, a slow or failing link, or an image that cannot be read all
// give a Brand without a logo (the PDF then draws a monogram). A download never fails because of the logo.
package gymbrand

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // decoders of the formats atm-storage-api previews
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/export"
)

const (
	// FetchTimeout bounds the download of a logo.
	FetchTimeout = 4 * time.Second
	// MaxLogoBytes is the largest logo accepted.
	MaxLogoBytes = 4 << 20
	// maxSide is the longest side the logo is scaled down to (plenty for a 42 mm wide letterhead).
	maxSide = 600
	// cacheSize bounds the in-memory logos (one per signed link).
	cacheSize = 128
)

// ErrNotFound: the gym does not exist.
var ErrNotFound = errors.New("gym not found")

// LogoSource returns a temporary address of the logo of a gym (its company in atm-auth-api), or nil.
type LogoSource interface {
	LogoURL(ctx context.Context, tenantID uuid.UUID) *string
}

// Reader builds brands.
type Reader struct {
	pool   *pgxpool.Pool
	logos  LogoSource
	client *http.Client
	log    *slog.Logger

	mu    sync.Mutex
	cache map[string][]byte
}

// New builds a reader; logos may be nil (no logo service configured).
func New(pool *pgxpool.Pool, logos LogoSource, log *slog.Logger) *Reader {
	if log == nil {
		log = slog.Default()
	}
	return &Reader{pool: pool, logos: logos, client: &http.Client{Timeout: FetchTimeout}, log: log, cache: map[string][]byte{}}
}

// Brand returns the name, colour and (when available) logo of the gym.
func (r *Reader) Brand(ctx context.Context, gymID, tenantID uuid.UUID) (export.Brand, error) {
	var b export.Brand
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(display_name, name), brand_color FROM gyms WHERE id = $1`, gymID).Scan(&b.Name, &b.Color)
	if errors.Is(err, pgx.ErrNoRows) {
		return export.Brand{}, ErrNotFound
	}
	if err != nil {
		return export.Brand{}, err
	}
	if r.logos != nil {
		if url := r.logos.LogoURL(ctx, tenantID); url != nil && *url != "" {
			b.Logo = r.logo(ctx, *url)
		}
	}
	return b, nil
}

func (r *Reader) logo(ctx context.Context, url string) []byte {
	r.mu.Lock()
	cached, ok := r.cache[url]
	r.mu.Unlock()
	if ok {
		return cached
	}
	logo, err := r.fetch(ctx, url)
	if err != nil {
		// The link is signed: never log it.
		r.log.Warn("gym logo not drawn", "error", err)
		return nil
	}
	r.mu.Lock()
	if len(r.cache) >= cacheSize {
		clear(r.cache)
	}
	r.cache[url] = logo
	r.mu.Unlock()
	return logo
}

// fetch downloads the image and normalizes it to a PNG of at most maxSide pixels.
func (r *Reader) fetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("logo request invalid")
	}
	res, err := r.client.Do(req)
	if err != nil {
		return nil, errors.New("logo download failed")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("logo download: status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxLogoBytes+1))
	if err != nil {
		return nil, errors.New("logo download interrupted")
	}
	if len(raw) > MaxLogoBytes {
		return nil, errors.New("logo too large")
	}
	return Normalize(raw)
}

// Normalize decodes a PNG, JPEG, GIF or WebP image and re-encodes it as an 8-bit PNG (what the PDF library
// reads), scaled down so its longest side is at most maxSide pixels.
func Normalize(raw []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("logo decode: %w", err)
	}
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil, errors.New("logo is empty")
	}
	if longest := max(w, h); longest > maxSide {
		w, h = max(1, w*maxSide/longest), max(1, h*maxSide/longest)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w == bounds.Dx() && h == bounds.Dy() {
		draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Src, nil)
	}
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
