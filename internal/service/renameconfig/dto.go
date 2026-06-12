package renameconfig

import "github.com/vizvim/omnibus/internal/repository"

// The D-06/D-07/D-08 defaults — the user's literal Mylar settings, shipped verbatim on a
// fresh install so omnibus files comics exactly like the user's existing library.
const (
	defaultFolderFormat  = "$Publisher/$Series ($Year)"
	defaultFileFormat    = "$Series $VolumeY $Annual #$Issue ($monthname $Year)"
	defaultPaddingFormat = "00x"
)

// Config is the domain view of the renaming config the transport layer (and the
// post-process template engine) consumes. There are no secrets, so it is returned
// verbatim with no masking.
type Config struct {
	FolderFormat        string
	FileFormat          string
	RenameEnabled       bool
	IssuePaddingEnabled bool
	PaddingFormat       string
	ReplaceSpaces       bool
	Lowercase           bool
}

// Input carries the mutable fields for an update. It mirrors Config one-to-one (no
// write-only fields, unlike download-client's APIKey).
type Input struct {
	FolderFormat        string
	FileFormat          string
	RenameEnabled       bool
	IssuePaddingEnabled bool
	PaddingFormat       string
	ReplaceSpaces       bool
	Lowercase           bool
}

// DefaultRenameConfig returns the D-06/D-07/D-08 defaults — the user's literal Mylar
// settings. This is the config a fresh install Gets and the template engine renders
// against until the user edits it.
func DefaultRenameConfig() Config {
	return Config{
		FolderFormat:        defaultFolderFormat,
		FileFormat:          defaultFileFormat,
		RenameEnabled:       true,
		IssuePaddingEnabled: true,
		PaddingFormat:       defaultPaddingFormat,
		ReplaceSpaces:       false,
		Lowercase:           false,
	}
}

// configFromRow maps a persisted repository row to the domain Config.
func configFromRow(r repository.RenameConfigRow) Config {
	return Config{
		FolderFormat:        r.FolderFormat,
		FileFormat:          r.FileFormat,
		RenameEnabled:       r.RenameEnabled,
		IssuePaddingEnabled: r.IssuePaddingEnabled,
		PaddingFormat:       r.PaddingFormat,
		ReplaceSpaces:       r.ReplaceSpaces,
		Lowercase:           r.Lowercase,
	}
}
