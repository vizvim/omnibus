package series_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/service/series"
)

func TestNormalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw      string
		wantSort float64
		wantQual string
	}{
		{"7", 7.0, ""},
		{"7.1", 7.1, ""},
		{"7.INH", 7.0, "INH"},
		{"7.NOW", 7.0, "NOW"},
		{"½", 0.5, "half"},
		{"Annual 1", 1.0, "annual"},
		{"Annual 2", 2.0, "annual"},
		{"0", 0.0, ""},
		{"  12  ", 12.0, ""},
	}
	for _, tc := range cases {
		sort, qual := series.Normalize(tc.raw)
		require.InDelta(t, tc.wantSort, sort, 0.0001, "sort for %q", tc.raw)
		require.Equal(t, tc.wantQual, qual, "qual for %q", tc.raw)
	}
}

func TestNormalizeDistinctness(t *testing.T) {
	t.Parallel()
	// The load-bearing pitfall-#3 assertion: 7 and 7.INH must NOT collide.
	s1, q1 := series.Normalize("7")
	s2, q2 := series.Normalize("7.INH")
	require.InDelta(t, s1, s2, 0.0001, "same numeric base")
	require.NotEqual(t, q1, q2, "distinguished by qualifier")
	require.NotEqual(t, series.Key(s1, q1), series.Key(s2, q2), "composite keys differ")
}
