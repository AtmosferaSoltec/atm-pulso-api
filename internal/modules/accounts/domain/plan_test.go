package domain

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestExtras(t *testing.T) {
	for _, tc := range []struct {
		counts         []int
		gyms, branches int
	}{
		{nil, 0, 0}, {[]int{1}, 0, 0}, {[]int{2}, 0, 1}, {[]int{1, 1}, 1, 0}, {[]int{3, 2, 1}, 2, 3},
	} {
		gyms, branches := Extras(tc.counts)
		assert.Equal(t, tc.gyms, gyms)
		assert.Equal(t, tc.branches, branches)
	}
}
