// Package ruff owns Ruff registration, transport compatibility and diagnostic
// source mapping; the Python preparation profile selects, masks and filters
// its snippets. It consumes the prepared tools
// contract and never imports workflow traversal or application sessions. The
// bundled Ruff provider manifest's configuration schema validates tools.ruff
// settings, so configuration decoding does not depend on tool execution.
package ruff
