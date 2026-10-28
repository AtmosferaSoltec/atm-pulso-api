package security

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHashAndVerifyShareConcurrencyLimit(t *testing.T) {
	hasher := Argon2idHasher{}
	hash, err := hasher.Hash("test-password")
	require.NoError(t, err)
	for range cap(hashSlots) {
		hashSlots <- struct{}{}
	}
	var release sync.Once
	unblock := func() {
		release.Do(func() {
			for range cap(hashSlots) {
				<-hashSlots
			}
		})
	}
	defer unblock()
	done := make(chan error, 2)
	go func() { _, err := hasher.Hash("other-password"); done <- err }()
	go func() { _, err := hasher.Verify("test-password", hash); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("hashing bypassed the shared concurrency limit (err=%v)", err)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	for range 2 {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("hashing did not resume")
		}
	}
}
