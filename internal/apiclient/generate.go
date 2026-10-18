// SPDX-License-Identifier: Apache-2.0

// Package apiclient is the Go client for the hub API, generated from
// api/openapi.yaml. aicm-agent's MCP server uses it, so it is Apache-2.0.
package apiclient

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config ../../api/oapi-client.yaml ../../api/openapi.yaml
