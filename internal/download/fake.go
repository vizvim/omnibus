package download

import "context"

// FakeProvider is a network-free download provider for service tests. It satisfies every
// consumer seam (search.Submitter via Submit, tracking.Poller via Poll, postprocess.HistoryRemover
// via RemoveFromHistory). It records the last GrabRequest and returns a canned client reference
// (or a configured error), and serves scripted Poll results keyed by client ref so the poll job
// can be tested without SAB.
type FakeProvider struct {
	kind      string
	clientRef string
	err       error

	// LastRequest is the most recent GrabRequest passed to Submit (for assertions).
	LastRequest GrabRequest
	// Calls counts Submit invocations (to assert idempotency / call counts).
	Calls int

	// PollResults maps a client ref to the PollResult Poll returns for it (test seam).
	PollResults map[string]PollResult
	// PollErr, when set, makes Poll return it for every ref.
	PollErr error
	// Removed records the client refs passed to RemoveFromHistory (D-05 assertions).
	Removed []string
}

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

// SetPoll scripts the PollResult Poll returns for a given client ref.
func (f *FakeProvider) SetPoll(clientRef string, res PollResult) {
	if f.PollResults == nil {
		f.PollResults = map[string]PollResult{}
	}
	f.PollResults[clientRef] = res
}

// Poll returns the scripted result for clientRef (or PollNotReady if none is scripted).
func (f *FakeProvider) Poll(_ context.Context, clientRef string) (PollResult, error) {
	if f.PollErr != nil {
		return PollResult{}, f.PollErr
	}
	if res, ok := f.PollResults[clientRef]; ok {
		return res, nil
	}
	return PollResult{Status: PollNotReady}, nil
}

// RemoveFromHistory records the client ref it was asked to remove (D-05 assertions).
func (f *FakeProvider) RemoveFromHistory(_ context.Context, clientRef string) error {
	f.Removed = append(f.Removed, clientRef)
	return nil
}
