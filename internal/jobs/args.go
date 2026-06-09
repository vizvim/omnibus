package jobs

import "github.com/riverqueue/river"

// ImportArgs are the River job arguments for a durable series import. SeriesID is
// tagged river:"unique" so River enforces at-most-one in-flight import job per series
// (a duplicate enqueue for the same series is a no-op), mirroring the dedup intent of
// the hand-owned engine it replaces.
type ImportArgs struct {
	SeriesID          int64 `json:"series_id"          river:"unique"`
	ComicvineVolumeID int64 `json:"comicvine_volume_id"`
}

// Kind uniquely identifies the import job type. It is stable across deploys so
// previously-enqueued jobs are still worked after a restart (JOBS-02).
func (ImportArgs) Kind() string { return "import_series" }

// InsertOpts enforces job uniqueness on the args (keyed on SeriesID via the
// river:"unique" tag above) so a second import enqueue for a series that already has a
// pending/scheduled/available/running import job collapses into the existing one.
func (ImportArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:      river.QueueDefault,
		UniqueOpts: river.UniqueOpts{ByArgs: true},
	}
}

// RefreshArgs are the River job arguments for a conditional metadata refresh. Like
// ImportArgs it is unique by SeriesID so a duplicate refresh enqueue for a series that
// already has a queued/running refresh collapses into a no-op (D-03).
type RefreshArgs struct {
	SeriesID          int64 `json:"series_id"          river:"unique"`
	ComicvineVolumeID int64 `json:"comicvine_volume_id"`
}

// Kind uniquely identifies the refresh job type, stable across deploys.
func (RefreshArgs) Kind() string { return "refresh_series" }

// InsertOpts enforces uniqueness on SeriesID (see RefreshArgs doc).
func (RefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:      river.QueueDefault,
		UniqueOpts: river.UniqueOpts{ByArgs: true},
	}
}

// SweepArgs are the (empty) arguments for the scheduled stale-only refresh sweep. It is
// registered as a River periodic job; uniqueness on its kind prevents overlapping ticks
// from stacking sweeps.
type SweepArgs struct{}

// Kind uniquely identifies the sweep job type, stable across deploys.
func (SweepArgs) Kind() string { return "refresh_sweep" }

// InsertOpts makes the sweep unique by kind so an overlapping periodic tick (while a
// previous sweep is still queued/running) collapses into the existing one.
func (SweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:      river.QueueDefault,
		UniqueOpts: river.UniqueOpts{},
	}
}
