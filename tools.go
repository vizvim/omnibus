//go:build tools

// Package tools pins the versions of code-generation binaries the build depends
// on (proto -> Go via protoc-gen-go + protoc-gen-connect-go, SQL -> Go via sqlc)
// so they are tracked in go.mod and reproducible across machines. This file is
// never compiled into the application binary; the `tools` build tag excludes it
// from normal builds and it exists solely to keep `go mod tidy` from dropping the
// tool modules.
package tools

import (
	_ "connectrpc.com/connect/cmd/protoc-gen-connect-go"
	_ "github.com/sqlc-dev/sqlc/cmd/sqlc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
