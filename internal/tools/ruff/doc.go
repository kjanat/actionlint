// Package ruff owns Ruff registration, Python snippet preparation, transport
// compatibility and diagnostic source mapping. It consumes the prepared tools
// contract and never imports workflow traversal or application sessions. Ruff
// configuration validation lives in internal/config/tools/ruff so configuration
// decoding does not depend on tool execution.
package ruff
