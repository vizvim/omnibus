// Package transport holds the Connect RPC handlers and HTTP transport wiring.
// Handlers map RPCs to service-layer calls and never import repository, provider,
// or db packages directly (package-layout.md layer-ownership rule).
package transport

import (
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
)

// SeriesHandler implements the SeriesService Connect handler. In Phase 2 plan
// 02-01 it embeds UnimplementedSeriesServiceHandler so every RPC returns
// CodeUnimplemented — this proves the transport seam is wired end to end. Real
// service logic is injected in plan 02-04.
type SeriesHandler struct {
	omnibusv1connect.UnimplementedSeriesServiceHandler
}

// NewSeriesHandler returns a SeriesService handler. It currently takes no
// dependencies; plan 02-04 widens the signature to inject the series service.
func NewSeriesHandler() *SeriesHandler {
	return &SeriesHandler{}
}
