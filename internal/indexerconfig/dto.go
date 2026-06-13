package indexerconfig

import "github.com/vizvim/omnibus/internal/repository"

// Indexer is the domain view of an indexer the transport layer consumes. It has NO
// api-key field by design — the key is write-only and masked (D-17 / T-4-01).
type Indexer struct {
	ID         int64
	Name       string
	Kind       string
	BaseURL    string
	Enabled    bool
	Categories string
	Priority   int32
	UseForRSS  bool
	CreatedAt  string
	UpdatedAt  string
}

// Input carries the mutable fields for create/update. APIKey is accepted here but
// never surfaced back out via the Indexer domain type.
type Input struct {
	Name       string
	Kind       string
	BaseURL    string
	APIKey     string
	Enabled    bool
	Categories string
	Priority   int32
	UseForRSS  bool
}

// indexerFromRow maps a repository row to the masked domain type (drops APIKey).
func indexerFromRow(r repository.IndexerRow) Indexer {
	return Indexer{
		ID:         r.ID,
		Name:       r.Name,
		Kind:       r.Kind,
		BaseURL:    r.BaseURL,
		Enabled:    r.Enabled,
		Categories: r.Categories,
		Priority:   r.Priority,
		UseForRSS:  r.UseForRSS,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}
