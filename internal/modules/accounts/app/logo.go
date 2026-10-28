package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

// MaxLogoBytes is the largest logo accepted.
const MaxLogoBytes = 10 << 20

// logoTypes are the accepted images, by their real content (never by the file name or the
// declared type). SVG is left out on purpose: it can carry scripts.
var logoTypes = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}

// LogoFile is a stored logo being read; the caller closes Body.
type LogoFile struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

// LogoStore keeps the logo files (Cloudflare R2, a private bucket). Objects are immutable: a new
// logo gets a new key. Browsers never read the bucket: the API serves the logo (see LogoFile).
type LogoStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	Delete(ctx context.Context, key string) error
	// Get returns domain.ErrResourceNotFound when the key does not exist.
	Get(ctx context.Context, key string) (LogoFile, error)
}

// logoVersion identifies one upload: the key without its folder and extension.
func logoVersion(key string) string {
	base := path.Base(key)
	return strings.TrimSuffix(base, path.Ext(base))
}

// LogoPath is the address of a gym's logo, relative to the API prefix. The version changes with
// every upload, so the file can be cached for good.
func LogoPath(gymID uuid.UUID, key string) string {
	return fmt.Sprintf("/gyms/%s/logo?v=%s", gymID, logoVersion(key))
}

func (s *Service) logoURL(gymID uuid.UUID, key *string) *string {
	if key == nil || s.Logos == nil {
		return nil
	}
	url := LogoPath(gymID, *key)
	return &url
}

// LogoFile opens the gym's current logo for the public logo endpoint. Cacheable says whether
// version is the current one, in which case the answer never changes.
func (s *Service) LogoFile(ctx context.Context, gymID uuid.UUID, version string) (file LogoFile, cacheable bool, err error) {
	if s.Logos == nil {
		return LogoFile{}, false, domain.ErrStorageUnavailable
	}
	key, err := s.Gyms.LogoKey(ctx, gymID)
	if err != nil {
		return LogoFile{}, false, err
	}
	if key == nil {
		return LogoFile{}, false, domain.ErrResourceNotFound
	}
	file, err = s.Logos.Get(ctx, *key)
	return file, version == logoVersion(*key), err
}

// logoManager resolves the gym a logo change targets. Only the superadmin sets the identity.
func (s *Service) logoManager(ctx context.Context, p auth.Principal, gymID uuid.UUID) (uuid.UUID, error) {
	if p.Role != auth.RoleSuperadmin {
		return uuid.Nil, domain.ErrForbidden
	}
	if s.Logos == nil {
		return uuid.Nil, domain.ErrStorageUnavailable
	}
	return s.Gyms.GymOrganization(ctx, gymID)
}

// SetLogo replaces the gym's logo with the image, checked by its content.
func (s *Service) SetLogo(ctx context.Context, p auth.Principal, gymID uuid.UUID, data []byte) (Gym, error) {
	org, err := s.logoManager(ctx, p, gymID)
	if err != nil {
		return Gym{}, err
	}
	if len(data) == 0 || len(data) > MaxLogoBytes {
		return Gym{}, fieldError("logo", "LOGO_SIZE_INVALID")
	}
	contentType := http.DetectContentType(data)
	extension, ok := logoTypes[contentType]
	if !ok {
		return Gym{}, fieldError("logo", "LOGO_TYPE_INVALID")
	}
	key := fmt.Sprintf("logos/%s/%s.%s", gymID, ids.New(), extension)
	if err := s.Logos.Put(ctx, key, contentType, data); err != nil {
		return Gym{}, fmt.Errorf("store logo: %w", err)
	}
	previous, err := s.Gyms.SetLogoKey(ctx, org, gymID, &key)
	if err != nil {
		s.discard(key)
		return Gym{}, err
	}
	if previous != nil {
		s.discard(*previous)
	}
	return s.gymOf(ctx, org, gymID)
}

// RemoveLogo clears the gym's logo.
func (s *Service) RemoveLogo(ctx context.Context, p auth.Principal, gymID uuid.UUID) (Gym, error) {
	org, err := s.logoManager(ctx, p, gymID)
	if err != nil {
		return Gym{}, err
	}
	previous, err := s.Gyms.SetLogoKey(ctx, org, gymID, nil)
	if err != nil {
		return Gym{}, err
	}
	if previous != nil {
		s.discard(*previous)
	}
	return s.gymOf(ctx, org, gymID)
}

// discard deletes an object nobody refers to any more. A failure only leaves an orphan
// file in the bucket, so it is logged and never fails the request.
func (s *Service) discard(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.Logos.Delete(ctx, key); err != nil {
		slog.Warn("could not delete a logo from the object store", "key", key, "error", err)
	}
}

func (s *Service) gymOf(ctx context.Context, org, gymID uuid.UUID) (Gym, error) {
	catalog, err := s.Catalogs.Catalog(ctx, org)
	if err != nil {
		return Gym{}, err
	}
	for _, gym := range catalog.Gyms {
		if gym.ID == gymID {
			gym.LogoURL = s.logoURL(gym.ID, gym.LogoKey)
			return gym, nil
		}
	}
	return Gym{}, domain.ErrResourceNotFound
}
