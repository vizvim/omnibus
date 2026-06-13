package postprocess_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/postprocess"
)

const (
	defaultFolderFormat = "$Publisher/$Series ($Year)"
	defaultFileFormat   = "$Series $VolumeY $Annual #$Issue ($monthname $Year)"
)

func TestRenderFolder(t *testing.T) {
	t.Parallel()

	got := postprocess.Render(defaultFolderFormat, map[string]string{
		postprocess.TokenPublisher: "DC Comics",
		postprocess.TokenSeries:    "Action Comics",
		postprocess.TokenYear:      "2011",
	}, postprocess.DefaultTemplateOpts())

	require.Equal(t, "DC Comics/Action Comics (2011)", got)
}

func TestRenderFolderDropsEmptyYear(t *testing.T) {
	t.Parallel()

	// $Year empty → the "()" pair is dropped (graceful omission).
	got := postprocess.Render(defaultFolderFormat, map[string]string{
		postprocess.TokenPublisher: "DC Comics",
		postprocess.TokenSeries:    "Action Comics",
	}, postprocess.DefaultTemplateOpts())

	require.Equal(t, "DC Comics/Action Comics", got)
}

func TestRenderFile(t *testing.T) {
	t.Parallel()

	// The D-06 default file format with $Annual empty → its segment drops and the
	// double space collapses; $Issue "1" pads to "001".
	got := postprocess.Render(defaultFileFormat, map[string]string{
		postprocess.TokenSeries:    "Action Comics",
		postprocess.TokenVolumeY:   "2011",
		postprocess.TokenIssue:     "1",
		postprocess.TokenMonthName: "June",
		postprocess.TokenYear:      "2011",
	}, postprocess.DefaultTemplateOpts())

	require.Equal(t, "Action Comics 2011 #001 (June 2011)", got)
}

func TestRenderFileWithAnnual(t *testing.T) {
	t.Parallel()

	got := postprocess.Render(defaultFileFormat, map[string]string{
		postprocess.TokenSeries:    "Action Comics",
		postprocess.TokenVolumeY:   "2011",
		postprocess.TokenAnnual:    "Annual",
		postprocess.TokenIssue:     "1",
		postprocess.TokenMonthName: "June",
		postprocess.TokenYear:      "2011",
	}, postprocess.DefaultTemplateOpts())

	require.Equal(t, "Action Comics 2011 Annual #001 (June 2011)", got)
}

func TestRenderMonthNameBeforeMonth(t *testing.T) {
	t.Parallel()

	// $monthname must be substituted before $month (it is a prefix). Both present.
	got := postprocess.Render("$monthname/$month", map[string]string{
		postprocess.TokenMonthName: "June",
		postprocess.TokenMonth:     "06",
	}, postprocess.DefaultTemplateOpts())

	require.Equal(t, "June/06", got)
}

func TestRenderGracefulOmissionCollapsesWhitespace(t *testing.T) {
	t.Parallel()

	// Multiple empty tokens leave runs of whitespace and an empty "[]" that must all
	// collapse/drop.
	got := postprocess.Render("$Series [$Imprint] $VolumeN", map[string]string{
		postprocess.TokenSeries: "Saga",
	}, postprocess.DefaultTemplateOpts())

	require.Equal(t, "Saga", got)
}

func TestRenderDropsDanglingHashWhenIssueEmpty(t *testing.T) {
	t.Parallel()

	got := postprocess.Render("$Series #$Issue", map[string]string{
		postprocess.TokenSeries: "Saga",
	}, postprocess.DefaultTemplateOpts())

	require.Equal(t, "Saga", got)
}

func TestRenderSanitizesIllegalCharsInValues(t *testing.T) {
	t.Parallel()

	// A "/" inside a token value must NOT create a path separator — it becomes "-".
	// ":" is stripped. The template's own "/" separators are preserved.
	got := postprocess.Render("$Publisher/$Series", map[string]string{
		postprocess.TokenPublisher: "Marvel: Comics",
		postprocess.TokenSeries:    "Spider-Man/Deadpool",
	}, postprocess.DefaultTemplateOpts())

	require.Equal(t, "Marvel Comics/Spider-Man-Deadpool", got)
}

func TestRenderReplaceSpaces(t *testing.T) {
	t.Parallel()

	opts := postprocess.DefaultTemplateOpts()
	opts.ReplaceSpaces = true

	got := postprocess.Render("$Series ($Year)", map[string]string{
		postprocess.TokenSeries: "Action Comics",
		postprocess.TokenYear:   "2011",
	}, opts)

	require.Equal(t, "Action_Comics_(2011)", got)
}

func TestRenderLowercase(t *testing.T) {
	t.Parallel()

	opts := postprocess.DefaultTemplateOpts()
	opts.Lowercase = true

	got := postprocess.Render("$Series", map[string]string{
		postprocess.TokenSeries: "Action Comics",
	}, opts)

	require.Equal(t, "action comics", got)
}

func TestPadIssue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		raw   string
		width int
		want  string
	}{
		{"single digit to 3", "1", 3, "001"},
		{"two digits to 3", "12", 3, "012"},
		{"three digits unchanged", "100", 3, "100"},
		{"four digits over width", "1000", 3, "1000"},
		{"width 2", "7", 2, "07"},
		{"width 1 (no pad)", "7", 1, "7"},
		{"text qualifier preserved", "7.INH", 3, "007.INH"},
		{"numeric decimal preserved", "1.5", 3, "001.5"},
		{"vulgar half preserved", "½", 3, "½"},
		{"empty stays empty", "", 3, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, postprocess.PadIssue(tc.raw, tc.width))
		})
	}
}

func TestRenderPaddingDisabled(t *testing.T) {
	t.Parallel()

	opts := postprocess.DefaultTemplateOpts()
	opts.PaddingEnabled = false

	got := postprocess.Render("#$Issue", map[string]string{
		postprocess.TokenIssue: "1",
	}, opts)

	require.Equal(t, "#1", got)
}

func TestRenderPaddingFormat2Digit(t *testing.T) {
	t.Parallel()

	opts := postprocess.DefaultTemplateOpts()
	opts.PaddingFormat = "0x"

	got := postprocess.Render("#$Issue", map[string]string{
		postprocess.TokenIssue: "1",
	}, opts)

	require.Equal(t, "#01", got)
}
