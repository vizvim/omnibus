package indexer

import "strings"

// normalizeBaseURL canonicalizes an indexer base URL so it always builds a valid
// http.Request. It trims surrounding whitespace; an empty result stays empty (callers
// apply their own default afterward). When the value carries no scheme it is prefixed
// with "http://" (a scheme-less "host:port" otherwise breaks http.NewRequest with
// "first path segment in URL cannot contain colon"). A URL that already has a scheme is
// left untouched. Finally any trailing slashes are trimmed so endpoint concatenation is
// deterministic.
func normalizeBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	return strings.TrimRight(s, "/")
}
