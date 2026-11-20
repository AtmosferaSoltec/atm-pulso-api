package app

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// purgeRepo keeps voucher rows in memory; only what the purge needs.
type purgeRepo struct {
	Repository
	rows   []purgeRow
	before time.Time
}

type purgeRow struct {
	Purgeable
	created time.Time
	purged  *time.Time
}

func (r *purgeRepo) Purgeable(_ context.Context, before time.Time, limit int) ([]Purgeable, error) {
	r.before = before
	sort.Slice(r.rows, func(i, j int) bool { return r.rows[i].created.Before(r.rows[j].created) })
	out := []Purgeable{}
	for _, row := range r.rows {
		if row.purged == nil && row.created.Before(before) && len(out) < limit {
			out = append(out, row.Purgeable)
		}
	}
	return out, nil
}

func (r *purgeRepo) MarkPurged(_ context.Context, vouchers []Purgeable, at time.Time) error {
	for _, v := range vouchers {
		for i := range r.rows {
			if r.rows[i].ID == v.ID {
				r.rows[i].purged = &at
			}
		}
	}
	return nil
}

// purgeStore records deletions; keys in fail answer an error, keys in missing answer "not found".
type purgeStore struct {
	Store
	deleted       []string
	fail, missing map[string]bool
}

func (s *purgeStore) Delete(_ context.Context, key string) error {
	switch {
	case s.fail[key]:
		return errors.New("r2 is down")
	case s.missing[key]:
		return domain.ErrVoucherNotFound
	}
	s.deleted = append(s.deleted, key)
	return nil
}

func TestPurgeDeletesExpiredFilesInBatchesAndKeepsFailuresForLater(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	repo := &purgeRepo{}
	add := func(key string, age time.Duration, kind domain.OwnerKind) {
		repo.rows = append(repo.rows, purgeRow{Purgeable: Purgeable{ID: uuid.New(), Kind: kind, StorageKey: key}, created: now.Add(-age)})
	}
	day := 24 * time.Hour
	for i, key := range []string{"a", "b", "c", "d", "e"} {
		add(key, time.Duration(100+i)*day, domain.OwnerPayment)
	}
	add("sale", 95*day, domain.OwnerSale)
	add("gone", 91*day, domain.OwnerCollection)
	add("broken", 120*day, domain.OwnerPayment)
	add("young", 89*day, domain.OwnerPayment)
	store := &purgeStore{fail: map[string]bool{"broken": true}, missing: map[string]bool{"gone": true}}
	service := &Service{Repo: repo, Store: store, Clock: clock.Fixed{At: now}, Retention: domain.RetentionDays(90), PurgeBatch: 3}

	purged, err := service.Purge(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 7, purged, "five payments, the sale, and the collection whose file was already missing")
	assert.Equal(t, now.Add(-90*day), repo.before)
	assert.ElementsMatch(t, []string{"a", "b", "c", "d", "e", "sale"}, store.deleted)
	for _, row := range repo.rows {
		switch row.StorageKey {
		case "broken", "young":
			assert.Nil(t, row.purged, row.StorageKey)
		default:
			require.NotNil(t, row.purged, row.StorageKey)
			assert.Equal(t, now, *row.purged)
		}
	}

	// The failed one is retried on the next run, and only it.
	delete(store.fail, "broken")
	store.deleted = nil
	purged, err = service.Purge(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, purged)
	assert.Equal(t, []string{"broken"}, store.deleted)
}

func TestPurgeDoesNothingWithoutStoreOrRetention(t *testing.T) {
	repo := &purgeRepo{rows: []purgeRow{{Purgeable: Purgeable{ID: uuid.New(), StorageKey: "old"}, created: time.Unix(0, 0)}}}
	store := &purgeStore{}
	for _, service := range []*Service{
		{Repo: repo, Store: store, Retention: domain.RetentionDays(0)},
		{Repo: repo, Retention: domain.RetentionDays(90)},
	} {
		purged, err := service.Purge(context.Background())
		require.NoError(t, err)
		assert.Zero(t, purged)
	}
	assert.Empty(t, store.deleted)
	assert.Nil(t, repo.rows[0].purged)
}
