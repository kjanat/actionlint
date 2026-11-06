// Package workspace loads action manifests from the repository workspace.
// Cache stores parsed metadata and captured source references. Resolver uses
// checkout placement, source mappings and environment changes to determine which
// files a call uses.
package workspace
