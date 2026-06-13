package indexer

import "context"

// FakeProvider is a configurable, network-free IndexerProvider for downstream service
// tests (mirrors metadata.FakeProvider). It returns its configured candidates for both
// Search and Feed, or a configured error.
type FakeProvider struct {
	kind       string
	candidates []Candidate
	err        error
}

var _ IndexerProvider = (*FakeProvider)(nil)

// NewFakeProvider builds a fake that returns the given candidates for Search/Feed.
func NewFakeProvider(kind string, candidates []Candidate) *FakeProvider {
	if kind == "" {
		kind = "fake"
	}
	return &FakeProvider{kind: kind, candidates: candidates}
}

// NewFailingFakeProvider builds a fake whose Search/Feed always return err (to exercise
// the gateway's skip-on-error behavior).
func NewFailingFakeProvider(kind string, err error) *FakeProvider {
	if kind == "" {
		kind = "fake"
	}
	return &FakeProvider{kind: kind, err: err}
}

// Kind reports the configured provider kind.
func (f *FakeProvider) Kind() string { return f.kind }

// Search returns the configured candidates (or error).
func (f *FakeProvider) Search(_ context.Context, _ string) ([]Candidate, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.candidates, nil
}

// Feed returns the configured candidates (or error).
func (f *FakeProvider) Feed(_ context.Context) ([]Candidate, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.candidates, nil
}
