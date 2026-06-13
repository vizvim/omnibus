package ddlconfig

// Config is the domain view of the DDL config the transport layer consumes. There are no
// secrets, so it is returned verbatim with no masking.
type Config struct {
	Enabled bool
}

// Input carries the mutable field for an update — the lone enable toggle.
type Input struct {
	Enabled bool
}

// DefaultDDLConfig returns the default-OFF posture a fresh install Gets. DDL is opt-in
// (locked decision MODEL): GetComics is consulted only after the user explicitly enables
// it.
func DefaultDDLConfig() Config {
	return Config{Enabled: false}
}
