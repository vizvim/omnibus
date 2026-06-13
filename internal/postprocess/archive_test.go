package postprocess

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// validRAR4 is a minimal, hand-crafted, structurally-valid RAR4 archive containing one
// stored file ("1.txt" → "hi"). It is verified to open + read its first header via
// nwaples/rardecode/v2. Hand-crafted (no rar CLI dependency) so the test is hermetic.
const validRAR4Base64 = "UmFyIRoHAM+QcwAADQAAAAAAAACdzXQAgCUAAgAAAAIAAAAArCqT2AAAAAAUMAUAIAAAADEudHh0aGk="

// writeValidCBZ writes a structurally-valid zip (CBZ) with one entry and returns its path.
func writeValidCBZ(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "valid.cbz")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("001.jpg")
	require.NoError(t, err)
	_, err = w.Write([]byte("not-really-a-jpeg-but-valid-bytes"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
	return path
}

// writeValidCBR writes the embedded valid RAR4 fixture and returns its path.
func writeValidCBR(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "valid.cbr")
	raw, err := base64.StdEncoding.DecodeString(validRAR4Base64)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	return path
}

// writeValidPDF writes a minimal "%PDF ... %%EOF" file and returns its path.
func writeValidPDF(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "valid.pdf")
	content := []byte("%PDF-1.7\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n")
	require.NoError(t, os.WriteFile(path, content, 0o600))
	return path
}

func TestValidateArchiveValidCBZ(t *testing.T) {
	t.Parallel()
	path := writeValidCBZ(t, t.TempDir())

	format, err := detectAndValidate(path)
	require.NoError(t, err)
	require.Equal(t, "cbz", format)
}

func TestValidateArchiveValidCBR(t *testing.T) {
	t.Parallel()
	path := writeValidCBR(t, t.TempDir())

	format, err := detectAndValidate(path)
	require.NoError(t, err)
	require.Equal(t, "cbr", format)
}

func TestValidateArchiveValidPDF(t *testing.T) {
	t.Parallel()
	path := writeValidPDF(t, t.TempDir())

	format, err := detectAndValidate(path)
	require.NoError(t, err)
	require.Equal(t, "pdf", format)
}

func TestValidateArchiveTruncatedCBZ(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	full := writeValidCBZ(t, dir)
	raw, err := os.ReadFile(full)
	require.NoError(t, err)

	// Keep the PK signature but lop off the central directory → corrupt zip.
	truncated := filepath.Join(dir, "truncated.cbz")
	require.NoError(t, os.WriteFile(truncated, raw[:len(raw)/2], 0o600))

	format, err := detectAndValidate(truncated)
	require.Error(t, err)
	require.Equal(t, "cbz", format)
}

func TestValidateArchiveTruncatedCBR(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	full := writeValidCBR(t, dir)
	raw, err := os.ReadFile(full)
	require.NoError(t, err)

	// Keep the Rar! signature but drop the file header/body → corrupt rar.
	truncated := filepath.Join(dir, "truncated.cbr")
	require.NoError(t, os.WriteFile(truncated, raw[:12], 0o600))

	format, err := detectAndValidate(truncated)
	require.Error(t, err)
	require.Equal(t, "cbr", format)
}

func TestValidateArchiveTruncatedPDF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// %PDF header but no %%EOF trailer.
	path := filepath.Join(dir, "truncated.pdf")
	require.NoError(t, os.WriteFile(path, []byte("%PDF-1.7\n1 0 obj<<>>endobj\n"), 0o600))

	format, err := detectAndValidate(path)
	require.Error(t, err)
	require.Equal(t, "pdf", format)
}

func TestValidateArchiveEmptyZipRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// An entry-less zip has no PK\x03\x04 local-file-header — its first bytes are the
	// end-of-central-directory record (PK\x05\x06). So a "comic" with zero pages is
	// rejected, here via the unrecognized-signature gate. Either way: not a valid CBZ.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	require.NoError(t, zw.Close())
	path := filepath.Join(dir, "empty.cbz")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	_, err := detectAndValidate(path)
	require.Error(t, err)
}

func TestValidateArchiveUnrecognizedSignature(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "mystery.bin")
	require.NoError(t, os.WriteFile(path, []byte("NOTAMAGIC........"), 0o600))

	format, err := detectAndValidate(path)
	require.ErrorIs(t, err, ErrUnrecognizedArchive)
	require.Empty(t, format)
}
