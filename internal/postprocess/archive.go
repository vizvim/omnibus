package postprocess

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	rardecode "github.com/nwaples/rardecode/v2"
)

// Archive format identifiers returned by detectAndValidate.
const (
	formatCBZ = "cbz"
	formatCBR = "cbr"
	formatPDF = "pdf"
)

// sigScanLimit caps the bytes read for the signature sniff (D-15: bounded read — never
// buffer the whole archive).
const sigScanLimit = 8

// trailerScanLimit caps the tail bytes scanned for the PDF "%%EOF" trailer. Real PDFs
// place %%EOF within the final bytes; 1 KiB is generous and bounded (anti-DoS, T-05-07).
const trailerScanLimit = 1024

// ErrUnrecognizedArchive is returned when a file's leading bytes match none of the
// supported comic-archive signatures.
var ErrUnrecognizedArchive = errors.New("unrecognized archive signature")

// detectAndValidate sniffs the leading magic bytes of path, then performs a structural
// open to confirm the archive is well-formed and not truncated (D-15). It is open-only:
// it reads the zip central directory / the first RAR header / the PDF header+trailer and
// NEVER extracts an entry (decompression-bomb and zip-slip avoidance — T-05-07/T-05-08;
// extraction/conversion is the deferred CONV-01). Returns the detected format and a
// non-nil error if the archive is corrupt, truncated, empty, or unrecognized.
func detectAndValidate(path string) (format string, err error) {
	sig, err := readFirstBytes(path, sigScanLimit)
	if err != nil {
		return "", fmt.Errorf("read signature: %w", err)
	}

	switch {
	case bytes.HasPrefix(sig, []byte("PK\x03\x04")):
		return formatCBZ, validateCBZ(path)
	case bytes.HasPrefix(sig, []byte("Rar!\x1a\x07")):
		return formatCBR, validateCBR(path)
	case bytes.HasPrefix(sig, []byte("%PDF")):
		return formatPDF, validatePDF(path)
	default:
		return "", ErrUnrecognizedArchive
	}
}

// validateCBZ opens the zip central directory (which confirms a non-truncated archive)
// and requires at least one entry.
func validateCBZ(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("corrupt cbz: %w", err)
	}
	defer func() { _ = r.Close() }()

	if len(r.File) == 0 {
		return errors.New("empty cbz")
	}
	return nil
}

// validateCBR opens the RAR read-only and reads the first header. A truncated or corrupt
// archive errors either at open or on the first Next(). It never decompresses an entry.
func validateCBR(path string) error {
	rc, err := rardecode.OpenReader(path)
	if err != nil {
		return fmt.Errorf("corrupt cbr: %w", err)
	}
	defer func() { _ = rc.Close() }()

	if _, err := rc.Next(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("corrupt cbr: %w", err)
	}
	return nil
}

// validatePDF confirms the "%PDF" header (already matched by the caller) is accompanied
// by a "%%EOF" trailer in the final bytes — the D-15 header+trailer check (no PDF
// library).
func validatePDF(path string) error {
	if !hasTrailer(path, []byte("%%EOF")) {
		return errors.New("truncated pdf: no %%EOF")
	}
	return nil
}

// readFirstBytes reads up to n leading bytes of path (bounded — never the whole file).
func readFirstBytes(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:read], nil
}

// hasTrailer reports whether the final trailerScanLimit bytes of path contain marker.
func hasTrailer(path string, marker []byte) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return false
	}

	size := info.Size()
	readLen := min(int64(trailerScanLimit), size)

	if _, err := f.Seek(size-readLen, io.SeekStart); err != nil {
		return false
	}

	tail := make([]byte, readLen)
	if _, err := io.ReadFull(f, tail); err != nil {
		return false
	}
	return bytes.Contains(tail, marker)
}
