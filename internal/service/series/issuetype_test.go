package series

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeriveIssueType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		raw        string
		title      string
		issueCount int
		want       string
	}{
		{"annual via normalized qual", "Annual 1", "", 50, "annual"},
		{"annual bare qualifier", "annual", "", 50, "annual"},
		{"annual whole word in title", "1", "2024 Annual Special", 50, "annual"},
		{"annual whole word in raw", "Annual 2", "", 50, "annual"},
		{"not annual when substring only", "1", "Perennial Tales", 50, "standard"},
		{"one-shot when single issue", "1", "The One", 1, "one-shot"},
		{"annual beats one-shot", "Annual 1", "", 1, "annual"},
		{"standard multi-issue", "7", "Some Issue", 50, "standard"},
		{"standard decimal", "7.5", "", 30, "standard"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, DeriveIssueType(tc.raw, tc.title, tc.issueCount))
		})
	}
}
