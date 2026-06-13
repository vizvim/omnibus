package postprocess

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeSrc creates a source file with content and returns its path.
func writeSrc(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "downloaded.cbz")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestImportHardlinkSameFilesystem(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "comic-bytes")
	dst := filepath.Join(root, "DC Comics", "Action Comics (2011)", "001.cbz")

	im := NewImporter()
	require.NoError(t, im.Import(src, dst, root))

	// dst exists with identical bytes.
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "comic-bytes", string(got))

	// It is a true hardlink: src and dst share an inode (same device + ino).
	si, err := os.Stat(src)
	require.NoError(t, err)
	di, err := os.Stat(dst)
	require.NoError(t, err)
	require.True(t, os.SameFile(si, di), "expected hardlink to share inode")
}

func TestImportEXDEVFallsBackToCopy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "cross-fs-bytes")
	dst := filepath.Join(root, "series", "001.cbz")

	// Inject a linker that simulates a cross-filesystem link error.
	im := &Importer{
		linker: func(_, _ string) error {
			return &os.LinkError{Op: "link", Err: syscall.EXDEV}
		},
	}

	require.NoError(t, im.Import(src, dst, root))

	// The copy fallback produced an independent file with identical content.
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "cross-fs-bytes", string(got))

	// It is NOT a hardlink (separate inode), proving the copy path ran.
	si, err := os.Stat(src)
	require.NoError(t, err)
	di, err := os.Stat(dst)
	require.NoError(t, err)
	require.False(t, os.SameFile(si, di), "expected a copy, not a hardlink")
}

func TestImportSurfacesNonEXDEVLinkError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "x")
	dst := filepath.Join(root, "001.cbz")

	sentinel := errors.New("permission denied")
	im := &Importer{
		linker: func(_, _ string) error {
			return &os.LinkError{Op: "link", Err: sentinel}
		},
	}

	err := im.Import(src, dst, root)
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)

	// No copy fallback ran → dst must not exist.
	_, statErr := os.Stat(dst)
	require.True(t, os.IsNotExist(statErr))
}

func TestImportRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "x")

	// A relative dst that escapes the root via "..".
	dst := filepath.Join("..", "..", "etc", "passwd")

	im := NewImporter()
	err := im.Import(src, dst, root)
	require.ErrorIs(t, err, ErrPathEscape)
}

func TestImportRejectsAbsolutePathOutsideRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "x")

	im := NewImporter()
	err := im.Import(src, "/tmp/evil/elsewhere.cbz", root)
	require.ErrorIs(t, err, ErrPathEscape)
}

func TestImportAcceptsAbsolutePathUnderRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "ok")
	dst := filepath.Join(root, "pub", "series", "001.cbz") // absolute, under root

	im := NewImporter()
	require.NoError(t, im.Import(src, dst, root))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "ok", string(got))
}

func TestImportIdempotentHardlinkSameFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "comic-bytes")
	dst := filepath.Join(root, "series", "001.cbz")

	im := NewImporter()
	require.NoError(t, im.Import(src, dst, root))
	// Re-import the identical source: a crash between Import and the DB commit
	// re-runs this; the second call must be a no-op success, not a no-clobber error.
	require.NoError(t, im.Import(src, dst, root))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "comic-bytes", string(got))

	// Still a true hardlink sharing src's inode.
	si, err := os.Stat(src)
	require.NoError(t, err)
	di, err := os.Stat(dst)
	require.NoError(t, err)
	require.True(t, os.SameFile(si, di), "expected hardlink to share inode after re-import")
}

func TestImportIdempotentEXDEVCopySameContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "cross-fs-bytes")
	dst := filepath.Join(root, "series", "001.cbz")

	im := &Importer{
		linker: func(_, _ string) error {
			return &os.LinkError{Op: "link", Err: syscall.EXDEV}
		},
	}
	require.NoError(t, im.Import(src, dst, root))
	// Re-import across the EXDEV copy path: the O_EXCL create now hits an existing
	// dst with matching content/size, so the second call returns nil.
	require.NoError(t, im.Import(src, dst, root))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "cross-fs-bytes", string(got))
}

func TestImportRejectsDifferentFileAtOccupiedDest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "original-bytes")
	dst := filepath.Join(root, "series", "001.cbz")

	im := NewImporter()
	require.NoError(t, im.Import(src, dst, root))

	// A genuinely different source (different bytes) at the occupied destination must
	// still be rejected — no-clobber is preserved.
	other := writeSrc(t, "different-bytes-entirely")
	err := im.Import(other, dst, root)
	require.Error(t, err)

	// The original file is untouched.
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "original-bytes", string(got))
}

func TestImportRejectsDifferentFileAtOccupiedDestEXDEV(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "original-bytes")
	dst := filepath.Join(root, "series", "001.cbz")

	exdev := func(_, _ string) error {
		return &os.LinkError{Op: "link", Err: syscall.EXDEV}
	}
	im := &Importer{linker: exdev}
	require.NoError(t, im.Import(src, dst, root))

	// Different content via the EXDEV copy path: the O_EXCL create hits an existing
	// dst whose content differs, so no-clobber must hold.
	other := writeSrc(t, "totally-other-bytes")
	err := im.Import(other, dst, root)
	require.Error(t, err)

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "original-bytes", string(got))
}

// TestImportFallback is the verification-named umbrella ensuring the hardlink→copy
// fallback contract holds end-to-end.
func TestImportFallback(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := writeSrc(t, "fallback")
	dst := filepath.Join(root, "001.cbz")

	im := &Importer{
		linker: func(_, _ string) error {
			return &os.LinkError{Op: "link", Err: syscall.EXDEV}
		},
	}
	require.NoError(t, im.Import(src, dst, root))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "fallback", string(got))
}
