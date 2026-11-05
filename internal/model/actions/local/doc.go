// Package local resolves local action dependencies. Cache shares parsed metadata
// and raw sources per repository; Resolver owns each workflow's mappings, checkout
// placement and environment uncertainty. Neither stores configuration, validation
// findings, traversal state or reporting models.
package local
