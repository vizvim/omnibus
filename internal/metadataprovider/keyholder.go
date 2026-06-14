package metadataprovider

import "sync/atomic"

// KeyHolder is a concurrency-safe holder for the live ComicVine API key. The service writes
// it on every successful Update; the ComicVine provider's keyFunc reads it on each request,
// so a saved key takes effect on the next metadata search with no restart. The key is held
// only here and is never logged.
type KeyHolder struct {
	key atomic.Pointer[string]
}

// NewKeyHolder builds a KeyHolder seeded with the given key.
func NewKeyHolder(initial string) *KeyHolder {
	h := &KeyHolder{}
	h.Set(initial)
	return h
}

// Set replaces the held key.
func (h *KeyHolder) Set(key string) {
	k := key
	h.key.Store(&k)
}

// Get returns the held key (empty string if never set). Suitable as the ComicVine provider's
// keyFunc.
func (h *KeyHolder) Get() string {
	if p := h.key.Load(); p != nil {
		return *p
	}
	return ""
}
