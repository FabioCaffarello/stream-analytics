//go:build tools

package tools

// This file is used to pin CLI/tool versions in `go.mod`. Do NOT import packages
// whose source is `package main` (programs) because they are not importable.
// Instead install the tool with `go install` and pin the module version in go.mod.
//
// To install the protoc plugin used in this repo:
//   go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
