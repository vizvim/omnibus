package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

// ConsoleHandler is a slog.Handler that writes human-friendly, optionally colorized
// log lines for local development — modeled on zerolog's console writer. Each record
// renders as:
//
//	15:04:05 INF message user=kyle count=3
//
// The time is dimmed, the level is a colored three-letter code, and attribute keys are
// faint. It is intended for a TTY; for production use the JSON handler. Color is enabled
// automatically when w is a terminal and NO_COLOR is unset, and can be forced either way
// via ConsoleOptions.
type ConsoleHandler struct {
	opts    ConsoleOptions
	w       io.Writer
	mu      *sync.Mutex // guards w; shared across With* derivations
	noColor bool
	groups  []string // open group chain; attr keys are prefixed by these joined with '.'
	// preformatted holds attrs attached via WithAttrs, already rendered (with leading
	// space) under the group chain that was open when they were added.
	preformatted string
}

// ConsoleOptions configures a ConsoleHandler. The zero value is valid: level defaults to
// info, time format to "15:04:05", and color is auto-detected from w.
type ConsoleOptions struct {
	// Level reports the minimum record level to log. Nil means slog.LevelInfo.
	Level slog.Leveler
	// AddSource includes the source file:line of the log call when true.
	AddSource bool
	// TimeFormat is the reference layout for the timestamp. Empty means "15:04:05".
	TimeFormat string
	// NoColor, when set, forces color on (false) or off (true) instead of auto-detecting.
	NoColor *bool
}

// ANSI styling helpers. Kept as a small palette so the renderer reads clearly.
const (
	ansiReset   = "\033[0m"
	ansiFaint   = "\033[2m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
)

// NewConsoleHandler builds a ConsoleHandler writing to w. Color is enabled when w is a
// terminal and the NO_COLOR environment variable is unset, unless opts.NoColor overrides.
func NewConsoleHandler(w io.Writer, opts *ConsoleOptions) *ConsoleHandler {
	var o ConsoleOptions
	if opts != nil {
		o = *opts
	}
	if o.TimeFormat == "" {
		o.TimeFormat = "15:04:05"
	}
	return &ConsoleHandler{
		opts:    o,
		w:       w,
		mu:      &sync.Mutex{},
		noColor: resolveNoColor(w, o.NoColor),
	}
}

// resolveNoColor decides whether to suppress color: an explicit override wins, otherwise
// color is on only for a TTY with NO_COLOR unset.
func resolveNoColor(w io.Writer, override *bool) bool {
	if override != nil {
		return *override
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return true
	}
	return !isatty.IsTerminal(f.Fd()) && !isatty.IsCygwinTerminal(f.Fd())
}

// Enabled reports whether records at level should be handled.
func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

// WithAttrs returns a handler that renders attrs ahead of each record's own attributes.
func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	var sb strings.Builder
	prefix := h.groupPrefix()
	for _, a := range attrs {
		h.appendAttr(&sb, prefix, a)
	}
	clone := *h
	clone.preformatted = h.preformatted + sb.String()
	return &clone
}

// WithGroup returns a handler that nests subsequent attribute keys under name.
func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(slices.Clone(h.groups), name)
	return &clone
}

// Handle renders one record: "time LEVEL message attrs".
func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder

	if !r.Time.IsZero() {
		sb.WriteString(h.color(ansiFaint, r.Time.Format(h.opts.TimeFormat)))
		sb.WriteByte(' ')
	}

	sb.WriteString(h.renderLevel(r.Level))
	sb.WriteByte(' ')
	sb.WriteString(r.Message)

	sb.WriteString(h.preformatted)

	prefix := h.groupPrefix()
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(&sb, prefix, a)
		return true
	})

	if h.opts.AddSource && r.PC != 0 {
		h.appendSource(&sb, r.PC)
	}

	sb.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, sb.String())
	return err
}

// renderLevel maps a level to a colored three-letter code (zerolog-style).
func (h *ConsoleHandler) renderLevel(level slog.Level) string {
	var code, ansi string
	switch {
	case level < slog.LevelInfo:
		code, ansi = "DBG", ansiMagenta
	case level < slog.LevelWarn:
		code, ansi = "INF", ansiGreen
	case level < slog.LevelError:
		code, ansi = "WRN", ansiYellow
	default:
		code, ansi = "ERR", ansiRed
	}
	// Levels offset from the canonical ones (e.g. INFO+2) get a numeric suffix so detail
	// is not silently lost.
	if base := canonicalLevel(level); level != base {
		code = fmt.Sprintf("%s%+d", code, level-base)
	}
	return h.color(ansi, code)
}

// canonicalLevel rounds a level down to the nearest named threshold.
func canonicalLevel(level slog.Level) slog.Level {
	switch {
	case level < slog.LevelInfo:
		return slog.LevelDebug
	case level < slog.LevelWarn:
		return slog.LevelInfo
	case level < slog.LevelError:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

// appendAttr renders " key=value", recursing into groups and resolving LogValuers. Keys
// are joined with the group prefix and faint-colored; values are quoted when they contain
// spaces or are empty.
func (h *ConsoleHandler) appendAttr(sb *strings.Builder, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}

	key := a.Key
	if prefix != "" && key != "" {
		key = prefix + "." + key
	}

	if a.Value.Kind() == slog.KindGroup {
		groupAttrs := a.Value.Group()
		if len(groupAttrs) == 0 {
			return
		}
		// An empty key promotes the group's attrs inline (slog semantics).
		nested := key
		if a.Key == "" {
			nested = prefix
		}
		for _, ga := range groupAttrs {
			h.appendAttr(sb, nested, ga)
		}
		return
	}

	sb.WriteByte(' ')
	sb.WriteString(h.color(ansiCyan, key))
	sb.WriteByte('=')
	sb.WriteString(h.renderValue(a.Value))
}

// renderValue formats an attribute value, quoting strings that need it and coloring
// errors red.
func (h *ConsoleHandler) renderValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return quoteIfNeeded(v.String())
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return h.color(ansiRed, quoteIfNeeded(err.Error()))
		}
		if m, ok := v.Any().(json.Marshaler); ok {
			if b, jerr := m.MarshalJSON(); jerr == nil {
				return string(b)
			}
		}
		return quoteIfNeeded(fmt.Sprint(v.Any()))
	default:
		return quoteIfNeeded(v.String())
	}
}

// appendSource renders " source=file:line" for the call site.
func (h *ConsoleHandler) appendSource(sb *strings.Builder, pc uintptr) {
	fs, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if fs.File == "" {
		return
	}
	sb.WriteByte(' ')
	sb.WriteString(h.color(ansiCyan, "source"))
	sb.WriteByte('=')
	sb.WriteString(fs.File + ":" + strconv.Itoa(fs.Line))
}

// groupPrefix joins the open group chain with '.'.
func (h *ConsoleHandler) groupPrefix() string {
	return strings.Join(h.groups, ".")
}

// color wraps s in an ANSI sequence unless color is disabled.
func (h *ConsoleHandler) color(ansi, s string) string {
	if h.noColor {
		return s
	}
	return ansi + s + ansiReset
}

// quoteIfNeeded wraps s in quotes when it is empty or contains whitespace/quotes so the
// key=value stream stays unambiguous, matching text/zerolog conventions.
func quoteIfNeeded(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\"=") {
		return strconv.Quote(s)
	}
	return s
}
