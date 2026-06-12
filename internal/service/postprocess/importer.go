package postprocess

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrPathEscape is returned when a destination path, after filepath.Clean, would resolve
// outside the library root — a path-traversal attempt (T-05-06). The import is rejected
// before any filesystem write.
var ErrPathEscape = errors.New("destination escapes library root")

// Importer moves a completed download into the library. It prefers an instant,
// space-efficient hardlink (os.Link) and transparently falls back to a byte copy when
// the source and destination are on different filesystems (EXDEV) — the common case for
// a separate download volume vs. the library volume.
//
// The linker is an injectable seam (default os.Link) so the EXDEV copy-fallback path is
// testable without a second real filesystem: a test substitutes a func returning a
// synthetic *os.LinkError{Err: syscall.EXDEV}.
type Importer struct {
	linker func(src, dst string) error
}

// NewImporter constructs an Importer using os.Link as the hardlinker.
func NewImporter() *Importer {
	return &Importer{linker: os.Link}
}

// Import places src at dst within root (the library path). It:
//  1. cleans dst and asserts it stays under root (rejects ".." escapes — T-05-06),
//  2. MkdirAll's the destination's parent directory,
//  3. hardlinks src→dst, falling back to a copy on EXDEV.
//
// A non-EXDEV link error (permissions, existing dst, missing parent) is surfaced. Import
// never overwrites silently across the copy path — it relies on os.Link/os.Create
// semantics for the caller's collision policy upstream.
func (im *Importer) Import(src, dst, root string) error {
	cleanDst, err := safeJoin(root, dst)
	if err != nil {
		return err
	}

	if mkErr := os.MkdirAll(filepath.Dir(cleanDst), 0o750); mkErr != nil {
		return fmt.Errorf("create library dir: %w", mkErr)
	}

	if linkErr := im.linker(src, cleanDst); linkErr == nil {
		return nil // instant hardlink (same filesystem)
	} else if !isEXDEV(linkErr) {
		// A real error (permissions, dst exists, etc.) — surface it.
		return fmt.Errorf("hardlink import: %w", linkErr)
	}

	// Cross-filesystem (EXDEV) → byte copy.
	if copyErr := copyFile(src, cleanDst); copyErr != nil {
		return fmt.Errorf("copy import: %w", copyErr)
	}
	return nil
}

// safeJoin cleans dst and guarantees the result stays within root. dst may be absolute
// (already under root) or relative (joined onto root). Any ".." traversal that escapes
// root is rejected with ErrPathEscape before any write occurs (T-05-06).
func safeJoin(root, dst string) (string, error) {
	cleanRoot := filepath.Clean(root)

	var candidate string
	if filepath.IsAbs(dst) {
		candidate = filepath.Clean(dst)
	} else {
		candidate = filepath.Clean(filepath.Join(cleanRoot, dst))
	}

	// Containment check with a boundary-safe prefix (cleanRoot + separator), so a
	// sibling like "/library-evil" cannot pass a naive "/library" prefix test.
	if candidate != cleanRoot && !strings.HasPrefix(candidate, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q not under %q", ErrPathEscape, candidate, cleanRoot)
	}
	return candidate, nil
}

// isEXDEV reports whether err is (or wraps) a cross-device link error. os.Link returns a
// *os.LinkError wrapping syscall.EXDEV; errors.Is sees through the wrapper.
func isEXDEV(err error) bool {
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	var le *os.LinkError
	return errors.As(err, &le) && errors.Is(le.Err, syscall.EXDEV)
}

// copyFile copies src to dst (os.Create + io.Copy + fsync + chmod-from-src), used when a
// hardlink is impossible across filesystems. fsync guarantees the bytes hit disk before
// the import is reported complete.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	// O_CREATE|O_EXCL mirrors os.Link's no-clobber semantics: an existing destination yields
	// os.ErrExist rather than a silent truncate (CR-02), so the copy fallback never destroys a
	// previously-filed issue on cross-filesystem (EXDEV) imports.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy bytes: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return fmt.Errorf("fsync dest: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close dest: %w", err)
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod dest: %w", err)
	}
	return nil
}
