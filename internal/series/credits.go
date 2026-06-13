package series

import "strings"

const (
	rolePenciler  = "penciler"
	rolePenciller = "penciller"
	roleCover     = "cover"
)

// NormalizeRole maps a raw ComicVine creator role to a normalized canonical role.
// Common synonyms collapse (penciler/penciller -> penciller); the well-known roles
// (writer, inker, colorist, letterer, editor, cover) pass through; any unrecognized
// role is preserved lowercased/trimmed rather than dropped, so no credit data is lost.
func NormalizeRole(cvRole string) string {
	r := strings.ToLower(strings.TrimSpace(cvRole))
	switch r {
	case rolePenciler, rolePenciller:
		return rolePenciller
	case roleCover, "cover artist":
		return roleCover
	default:
		return r
	}
}
