package downloadclient

import "github.com/vizvim/omnibus/internal/repository"

// Config is the masked domain view of the download-client config the transport layer
// consumes. It has NO api-key field by design — the key is write-only and masked
// (D-17 / T-ul5-01), mirroring indexer.Indexer.
type Config struct {
	URL        string
	Category   string
	Configured bool
}

// Input carries the mutable fields for an update. APIKey is accepted here but never
// surfaced back out via the Config domain type.
type Input struct {
	URL      string
	APIKey   string
	Category string
}

// configFromRow maps a repository row to the masked domain Config (drops APIKey and sets
// Configured = (URL != "")).
func configFromRow(r repository.DownloadClientRow) Config {
	return Config{
		URL:        r.URL,
		Category:   r.Category,
		Configured: r.URL != "",
	}
}
