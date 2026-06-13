package postprocess

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrPathEscape is returned when a destination path, after filepath.Clean, would resolve
// outside the library root — a path-traversal attempt. The import is rejected before any
// filesystem write.
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
//  1. cleans dst and asserts it stays under root (rejects ".." escapes),
//  2. MkdirAll's the destination's parent directory,
//  3. hardlinks src→dst, falling back to a copy on EXDEV.
//
// Import is idempotent for the identical file: re-importing the same src to an existing dst
// (after a crash between the import and its database commit) returns nil rather than failing
// on the occupied destination. A genuinely different file at an occupied destination is still
// rejected — no-clobber is preserved, so a previously-filed issue is never overwritten.
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
	} else if errors.Is(linkErr, os.ErrExist) {
		// The destination already exists. A prior run that created the same hardlink makes this
		// re-import a no-op; only a different file at the destination is an error.
		return im.resolveExisting(src, cleanDst, linkErr)
	} else if !isEXDEV(linkErr) {
		// A real error (permissions, missing parent, etc.) — surface it.
		return fmt.Errorf("hardlink import: %w", linkErr)
	}

	// Cross-filesystem (EXDEV) → byte copy.
	if copyErr := copyFile(src, cleanDst); copyErr != nil {
		if errors.Is(copyErr, os.ErrExist) {
			// The O_EXCL create hit an existing destination. Treat a content/size match as an
			// already-completed import (idempotent re-run); a mismatch keeps no-clobber.
			return im.resolveExisting(src, cleanDst, copyErr)
		}
		return fmt.Errorf("copy import: %w", copyErr)
	}
	return nil
}

// resolveExisting decides whether an "already exists" destination is the identical file from a
// prior import (return nil, idempotent) or a genuinely different file (return the existing error,
// preserving no-clobber). It first tries the cheap inode-identity check (os.SameFile, true when a
// prior run created the same hardlink) and falls back to a streaming content/size comparison for
// the copy case where inodes necessarily differ.
func (im *Importer) resolveExisting(src, dst string, existsErr error) error {
	si, srcErr := os.Stat(src)
	di, dstErr := os.Stat(dst)
	if srcErr == nil && dstErr == nil && os.SameFile(si, di) {
		return nil // a prior run already created this hardlink to the source
	}

	same, cmpErr := sameContent(src, dst)
	if cmpErr != nil {
		return fmt.Errorf("import: compare existing destination: %w", cmpErr)
	}
	if same {
		return nil // identical bytes already filed (copy/EXDEV re-import)
	}
	return fmt.Errorf("import: destination exists with different content: %w", existsErr)
}

// sameContent reports whether src and dst exist, have equal size, and hold identical bytes. It
// streams both files with bounded buffers rather than reading them whole into memory.
func sameContent(src, dst string) (bool, error) {
	si, err := os.Stat(src)
	if err != nil {
		return false, fmt.Errorf("stat source: %w", err)
	}
	di, err := os.Stat(dst)
	if err != nil {
		return false, fmt.Errorf("stat destination: %w", err)
	}
	if si.Size() != di.Size() {
		return false, nil
	}

	sf, err := os.Open(src)
	if err != nil {
		return false, fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = sf.Close() }()

	df, err := os.Open(dst)
	if err != nil {
		return false, fmt.Errorf("open destination: %w", err)
	}
	defer func() { _ = df.Close() }()

	const bufSize = 64 * 1024
	sbuf := make([]byte, bufSize)
	dbuf := make([]byte, bufSize)
	for {
		sn, serr := io.ReadFull(sf, sbuf)
		dn, derr := io.ReadFull(df, dbuf)
		if sn != dn || !bytes.Equal(sbuf[:sn], dbuf[:dn]) {
			return false, nil
		}
		if errors.Is(serr, io.EOF) || errors.Is(serr, io.ErrUnexpectedEOF) {
			// Sizes matched up front, so both readers reach the end together.
			return true, nil
		}
		if serr != nil {
			return false, fmt.Errorf("read source: %w", serr)
		}
		if derr != nil {
			return false, fmt.Errorf("read destination: %w", derr)
		}
	}
}

// safeJoin cleans dst and guarantees the result stays within root. dst may be absolute
// (already under root) or relative (joined onto root). Any ".." traversal that escapes
// root is rejected with ErrPathEscape before any write occurs.
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
	// os.ErrExist rather than a silent truncate, so the copy fallback never destroys a
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
