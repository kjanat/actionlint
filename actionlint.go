// Package actionlint provides the entry point for in-memory workflow analysis.
// Use analysis for filesystem discovery, config for configuration documents,
// syntax for workflow trees, and diagnostic for findings and suggested edits.
//
// The Go library follows the CLI release cycle; library API changes may be
// breaking. See docs/api.md for the package contract and migration guide.
package actionlint

import (
	"context"

	"actionlint.kjanat.dev/v2/analysis"
)

// CheckSources checks in-memory workflows and action manifests without rendering output.
func CheckSources(ctx context.Context, request analysis.Request) (*analysis.Result, error) {
	return analysis.CheckSources(ctx, request)
}
