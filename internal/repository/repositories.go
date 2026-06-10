package repository

import "github.com/vizvim/omnibus/internal/db"

// Repositories bundles every domain repository so the wiring layer can construct them
// once and hand the set to the service layer.
type Repositories struct {
	Series         SeriesRepository
	Issue          IssueRepository
	Cover          CoverRepository
	Publisher      PublisherRepository
	Arc            ArcRepository
	MetadataCache  MetadataCacheRepository
	UserConfig     UserConfigRepository
	Indexers       IndexerRepository
	Downloads      DownloadRepository
	IssueEvents    IssueEventRepository
	DownloadClient DownloadClientRepository
}

// NewRepositories constructs all repositories bound to the read/write pools.
func NewRepositories(d *db.DB) *Repositories {
	return &Repositories{
		Series:         NewSeriesRepository(d),
		Issue:          NewIssueRepository(d),
		Cover:          NewCoverRepository(d),
		Publisher:      NewPublisherRepository(d),
		Arc:            NewArcRepository(d),
		MetadataCache:  NewMetadataCacheRepository(d),
		UserConfig:     NewUserConfigRepository(d),
		Indexers:       NewIndexerRepository(d),
		Downloads:      NewDownloadRepository(d),
		IssueEvents:    NewIssueEventRepository(d),
		DownloadClient: NewDownloadClientRepository(d),
	}
}
