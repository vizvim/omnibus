package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// RenameConfig user_config keys. The renaming config is stored as a small set of
// key-value rows in the existing user_config table rather than a dedicated table
// (D-09 explicitly allows this — lighter for a multi-field config).
const (
	keyRenameFolderFormat  = "rename.folder_format"
	keyRenameFileFormat    = "rename.file_format"
	keyRenameEnabled       = "rename.rename_enabled"
	keyRenameIssuePadding  = "rename.issue_padding_enabled"
	keyRenamePaddingFormat = "rename.padding_format"
	keyRenameReplaceSpaces = "rename.replace_spaces"
	keyRenameLowercase     = "rename.lowercase"
)

// boolTrue is the stored representation of a true boolean user_config value.
const boolTrue = "true"

// RenameConfigRow is the domain view of the renaming config persisted across several
// user_config keys. Booleans are stored as "true"/"false" strings. Absent keys are
// reported via the per-field *Set flags so the service can fall back to its defaults
// only for the keys that have never been written.
type RenameConfigRow struct {
	FolderFormat        string
	FileFormat          string
	RenameEnabled       bool
	IssuePaddingEnabled bool
	PaddingFormat       string
	ReplaceSpaces       bool
	Lowercase           bool

	// Present is false when NO renaming key has ever been written (fresh DB). The
	// service uses this to return the full D-06 defaults rather than zero values.
	Present bool
}

// RenameConfigUpsert carries the full set of renaming fields to persist.
type RenameConfigUpsert struct {
	FolderFormat        string
	FileFormat          string
	RenameEnabled       bool
	IssuePaddingEnabled bool
	PaddingFormat       string
	ReplaceSpaces       bool
	Lowercase           bool
}

// RenameConfigRepository persists the renaming config over user_config keys. Reads go
// through the read pool, writes through the single-writer pool (ADR 0002).
type RenameConfigRepository interface {
	// Get loads each renaming key. When no renaming key has been written yet the
	// returned row has Present == false (the service then supplies the D-06 defaults).
	Get(ctx context.Context) (RenameConfigRow, error)
	// Upsert persists every renaming field as a user_config key.
	Upsert(ctx context.Context, in RenameConfigUpsert) (RenameConfigRow, error)
}

type renameConfigRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewRenameConfigRepository binds a RenameConfigRepository to the read and write pools.
func NewRenameConfigRepository(d *db.DB) RenameConfigRepository {
	return &renameConfigRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

// getKey loads a single user_config value, reporting absence via (value, present, err).
func (r *renameConfigRepository) getKey(ctx context.Context, key string) (string, bool, error) {
	row, err := r.read.GetUserConfig(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return row.Value, true, nil
}

func (r *renameConfigRepository) Get(ctx context.Context) (RenameConfigRow, error) {
	var out RenameConfigRow

	type field struct {
		key    string
		assign func(value string)
	}
	fields := []field{
		{keyRenameFolderFormat, func(v string) { out.FolderFormat = v }},
		{keyRenameFileFormat, func(v string) { out.FileFormat = v }},
		{keyRenameEnabled, func(v string) { out.RenameEnabled = v == boolTrue }},
		{keyRenameIssuePadding, func(v string) { out.IssuePaddingEnabled = v == boolTrue }},
		{keyRenamePaddingFormat, func(v string) { out.PaddingFormat = v }},
		{keyRenameReplaceSpaces, func(v string) { out.ReplaceSpaces = v == boolTrue }},
		{keyRenameLowercase, func(v string) { out.Lowercase = v == boolTrue }},
	}

	for _, f := range fields {
		value, present, err := r.getKey(ctx, f.key)
		if err != nil {
			return RenameConfigRow{}, err
		}
		if present {
			out.Present = true
			f.assign(value)
		}
	}

	return out, nil
}

func (r *renameConfigRepository) Upsert(ctx context.Context, in RenameConfigUpsert) (RenameConfigRow, error) {
	pairs := [][2]string{
		{keyRenameFolderFormat, in.FolderFormat},
		{keyRenameFileFormat, in.FileFormat},
		{keyRenameEnabled, boolToString(in.RenameEnabled)},
		{keyRenameIssuePadding, boolToString(in.IssuePaddingEnabled)},
		{keyRenamePaddingFormat, in.PaddingFormat},
		{keyRenameReplaceSpaces, boolToString(in.ReplaceSpaces)},
		{keyRenameLowercase, boolToString(in.Lowercase)},
	}

	for _, p := range pairs {
		if err := r.write.SetUserConfig(ctx, sqlc.SetUserConfigParams{Key: p[0], Value: p[1]}); err != nil {
			return RenameConfigRow{}, err
		}
	}

	return RenameConfigRow{
		FolderFormat:        in.FolderFormat,
		FileFormat:          in.FileFormat,
		RenameEnabled:       in.RenameEnabled,
		IssuePaddingEnabled: in.IssuePaddingEnabled,
		PaddingFormat:       in.PaddingFormat,
		ReplaceSpaces:       in.ReplaceSpaces,
		Lowercase:           in.Lowercase,
		Present:             true,
	}, nil
}

func boolToString(b bool) string {
	if b {
		return boolTrue
	}
	return "false"
}
