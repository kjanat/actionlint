package documentparse

// The generator follows the decoded Workflow type closure and discovers its
// closed interface implementations with go/types. No runtime reflection is used.
//go:generate go run ./internal/generate ../.. ./internal/frozen
