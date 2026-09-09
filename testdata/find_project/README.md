This directory is used for testing that actionlint can detect a repository root.

- `TestLintFindProjectFromPath` in `linter_test.go`
- `project_test.go`

Tests copy these files into a temporary directory and create a `.git` marker there for project discovery.
Go removes the temporary directory after the test, leaving the checked-in fixtures unchanged.
