package series

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vizvim/omnibus/internal/provider/metadata"
	"github.com/vizvim/omnibus/internal/repository"
)

func TestStripHTML(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                                    "",
		"plain text":                          "plain text",
		"<p>Hello</p>":                        "Hello",
		"<p><em><b>Bold</b></em> text</p>":    "Bold text",
		"A &amp; B &lt; C":                    "A & B < C",
		"<a href=\"x\">link</a>":              "link",
		"line<br/>break":                      "line break",
		"  spaced   <i>out</i>  &mdash;  end": "spaced out — end",
		"<br>":                                "",
	}
	for in, want := range cases {
		assert.Equal(t, want, stripHTML(in), "stripHTML(%q)", in)
	}
}

func TestNormalizeRole(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"writer":        "writer",
		"Writer":        "writer",
		"  inker  ":     "inker",
		"colorist":      "colorist",
		"letterer":      "letterer",
		"editor":        "editor",
		"penciler":      "penciller",
		"penciller":     "penciller",
		"Penciler":      "penciller",
		"cover":         "cover",
		"Cover":         "cover",
		"cover artist":  "cover",
		"plotter":       "plotter", // unknown passes through lowercased
		"Assistant Ed.": "assistant ed.",
		"":              "",
	}

	for in, want := range cases {
		assert.Equal(t, want, NormalizeRole(in), "NormalizeRole(%q)", in)
	}
}

func TestToRepoCredits(t *testing.T) {
	t.Parallel()

	got := toRepoCredits([]metadata.Credit{
		{Role: "writer", Name: "A", ComicvinePersonID: 1},
		{Role: "penciler", Name: "B", ComicvinePersonID: 2},  // normalizes to penciller
		{Role: "Penciller", Name: "B", ComicvinePersonID: 2}, // duplicate after normalize -> dropped
		{Role: "", Name: "C"},                                // empty role -> dropped
		{Role: "editor", Name: ""},                           // empty name -> dropped
	})

	assert.ElementsMatch(t, []repository.IssueCredit{
		{Role: "writer", Name: "A", CVPersonID: 1},
		{Role: "penciller", Name: "B", CVPersonID: 2},
	}, got)

	assert.Nil(t, toRepoCredits(nil))
}
