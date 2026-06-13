package indexer

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"scheme-less host:port", "192.168.69:5076", "http://192.168.69:5076"},
		{"scheme-less host:port trailing slash", "192.168.69:5076/", "http://192.168.69:5076"},
		{"https trailing slash trimmed", "https://nzb.test/", "https://nzb.test"},
		{"http unchanged", "http://nzb.test", "http://nzb.test"},
		{"empty stays empty", "", ""},
		{"whitespace only stays empty", "   ", ""},
		{"surrounding whitespace trimmed", "  nzb.test:9117  ", "http://nzb.test:9117"},
		{"https no trailing slash unchanged", "https://nzb.test", "https://nzb.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeBaseURL(tc.in); got != tc.want {
				t.Fatalf("normalizeBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
