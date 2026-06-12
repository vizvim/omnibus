package search

import (
	"reflect"
	"testing"
)

func TestCleanSeriesName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Saga", "Saga"},
		{"slash to space", "X/Men", "X Men"},
		{"dash to space", "Spider-Man", "Spider Man"},
		{"strip the", "The Boys", "Boys"},
		{"strip and", "Hawkeye and Mockingbird", "Hawkeye Mockingbird"},
		{"the and case-insensitive", "THE Walking Dead AND Friends", "Walking Dead Friends"},
		{"keep substrings of the", "Theseus Bandit", "Theseus Bandit"},
		{"strip ampersand colon question comma", "Archie: Love & Death? Yes,", "Archie Love Death Yes"},
		{"collapse whitespace", "Doom   Patrol", "Doom Patrol"},
		{"trim", "  Batman  ", "Batman"},
		{"combined", "The Amazing/Spider-Man: Vol & 1?", "Amazing Spider Man Vol 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cleanSeriesName(tc.in); got != tc.want {
				t.Fatalf("cleanSeriesName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIssueNumberVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"one digit", "7", []string{"007", "07", "7"}},
		{"zero", "0", []string{"000", "00", "0"}},
		{"two digits", "23", []string{"023", "23"}},
		{"three digits", "123", []string{"123"}},
		{"four digits", "1024", []string{"1024"}},
		{"one digit decimal", "7.5", []string{"007.5", "07.5", "7.5"}},
		{"two digit decimal", "23.5", []string{"023.5", "23.5"}},
		{"non-numeric", "AU", []string{"AU"}},
		{"qualifier suffix", "7.INH", []string{"7.INH"}},
		{"empty", "", []string{""}},
		{"whitespace", "  ", []string{"  "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := issueNumberVariants(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("issueNumberVariants(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestBuildQueries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		seriesName string
		raw        string
		issueType  string
		want       []string
	}{
		{
			name: "standard single digit", seriesName: "Saga", raw: "7", issueType: "standard",
			want: []string{"Saga 007", "Saga 07", "Saga 7"},
		},
		{
			name: "standard empty type defaults", seriesName: "Saga", raw: "23", issueType: "",
			want: []string{"Saga 023", "Saga 23"},
		},
		{
			name: "annual", seriesName: "Saga", raw: "3", issueType: "annual",
			want: []string{"Saga annual 003", "Saga annual 03", "Saga annual 3"},
		},
		{
			name: "one-shot ignores number", seriesName: "The Killing Joke", raw: "1", issueType: "one-shot",
			want: []string{"Killing Joke"},
		},
		{
			name: "name cleaned in query", seriesName: "Spider-Man", raw: "1", issueType: "standard",
			want: []string{"Spider Man 001", "Spider Man 01", "Spider Man 1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildQueries(tc.seriesName, tc.raw, tc.issueType)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("buildQueries(%q,%q,%q) = %v, want %v", tc.seriesName, tc.raw, tc.issueType, got, tc.want)
			}
		})
	}
}

func TestBuildQueriesDeduplicates(t *testing.T) {
	t.Parallel()
	// A 3+ digit number yields a single variant; build still returns one (no dup),
	// exercising the de-dup path with a name that cleans to a stable term.
	got := buildQueries("Saga", "123", "standard")
	want := []string{"Saga 123"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildQueries = %v, want %v", got, want)
	}
}
