package transport

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
)

// NewInterceptors returns the Connect interceptors for every RPC: a request-scoped
// slog interceptor and the OpenTelemetry interceptor. The OTel interceptor uses the
// global providers; wiring real exporters is a later concern.
func NewInterceptors(logger *slog.Logger) ([]connect.Interceptor, error) {
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		return nil, err
	}
	return []connect.Interceptor{
		newSlogInterceptor(logger),
		otelInterceptor,
	}, nil
}

// newSlogInterceptor logs each unary RPC with its procedure, duration, and error.
func newSlogInterceptor(logger *slog.Logger) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			start := time.Now()
			resp, err := next(ctx, req)
			attrs := []any{
				slog.String("procedure", req.Spec().Procedure),
				slog.Duration("duration", time.Since(start)),
			}
			if err != nil {
				logger.Error("rpc failed", append(attrs, slog.Any("error", err), slog.String("code", connect.CodeOf(err).String()))...)
			} else {
				logger.Info("rpc handled", attrs...)
			}
			return resp, err
		}
	}
}
