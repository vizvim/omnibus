package download

import "context"

// FakeProvider is a network-free DownloadProvider for service tests. It records the last
// GrabRequest and returns a canned client reference (or a configured error).
type FakeProvider struct {
	kind      string
	clientRef string
	err       error

	// LastRequest is the most recent GrabRequest passed to Submit (for assertions).
	LastRequest GrabRequest
	// Calls counts Submit invocations (to assert idempotency / call counts).
	Calls int
}

var _ DownloadProvider = (*FakeProvider)(nil)

// NewFakeProvider builds a fake that returns clientRef from Submit.
func NewFakeProvider(kind, clientRef string) *FakeProvider {
	if kind == "" {
		kind = "fake"
	}
	return &FakeProvider{kind: kind, clientRef: clientRef}
}

// NewFailingFakeProvider builds a fake whose Submit always returns err.
func NewFailingFakeProvider(kind string, err error) *FakeProvider {
	if kind == "" {
		kind = "fake"
	}
	return &FakeProvider{kind: kind, err: err}
}

// Kind reports the configured provider kind.
func (f *FakeProvider) Kind() string { return f.kind }

// Submit records the request and returns the canned ref (or error).
func (f *FakeProvider) Submit(_ context.Context, req GrabRequest) (string, error) {
	f.Calls++
	f.LastRequest = req
	if f.err != nil {
		return "", f.err
	}
	return f.clientRef, nil
}
