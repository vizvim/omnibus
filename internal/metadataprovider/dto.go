package metadataprovider

import "github.com/vizvim/omnibus/internal/repository"

// providerComicVine is the only metadata provider id today. It is present in the domain
// view so additional providers can be added later.
const providerComicVine = "comicvine"

// Config is the masked domain view of the metadata-provider config the transport layer
// consumes. It has NO api-key field by design — the key is write-only and masked
// (D-17 / T-ul5-01), mirroring downloadclient.Config.
type Config struct {
	Provider   string
	Configured bool
}

// Input carries the mutable fields for an update. APIKey is accepted here but never
// surfaced back out via the Config domain type.
type Input struct {
	Provider string
	APIKey   string
}

// configFromRow maps a repository row to the masked domain Config (drops APIKey and sets
// Configured = (APIKey != "")). Provider is fixed to "comicvine" today.
func configFromRow(r repository.ComicVineConfigRow) Config {
	return Config{
		Provider:   providerComicVine,
		Configured: r.APIKey != "",
	}
}
