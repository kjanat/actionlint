package inputs

import (
	"context"
	"encoding/json"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

// ToolIntegration advertises an optional, versioned host tool profile.
type ToolIntegration struct {
	Tool                string
	Contract            string
	ConfigurationSchema string
}

// ToolIntegrator negotiates configuration before replacing a native tool.
// NegotiateTool receives host profile configuration and caller-owned plugin
// options separately. Only the guest interprets or combines the latter.
// A nil descriptor means the optional interface is absent.
type ToolIntegrator interface {
	ToolIntegration() *ToolIntegration
	NegotiateTool(context.Context, json.RawMessage, json.RawMessage) (*ToolNegotiation, error)
}

type ToolResourceKind string

const (
	ToolWorkspace ToolResourceKind = "workspace"
	ToolDirectory ToolResourceKind = "directory"
)

// ToolResourceRequest requests a read-only workspace or directory grant.
// Path is populated only for ToolDirectory.
type ToolResourceRequest struct {
	Kind ToolResourceKind
	Path string
}

// ToolNegotiation contains accepted options and explicit filesystem grants.
type ToolNegotiation struct {
	Options   json.RawMessage
	Resources []ToolResourceRequest
}

// UnsupportedToolConfigurationError permits falling back to the native tool.
type UnsupportedToolConfigurationError struct{ Reason string }

func (e *UnsupportedToolConfigurationError) Error() string { return e.Reason }

// SourceRoot translates a source namespace to a granted WASI mount.
type SourceRoot struct {
	Host  string
	Guest string
}

type ShellScriptLineKind string

const (
	ShellScriptSourceLine    ShellScriptLineKind = "source"
	ShellScriptCommentLine   ShellScriptLineKind = "comment"
	ShellScriptGeneratedLine ShellScriptLineKind = "generated"
)

type ShellScriptLine struct {
	Kind     ShellScriptLineKind
	Line     int
	Position sourcepos.Position
}
type ShellScriptInput struct {
	Text       string
	MaskedText string
	Dialect    *string
	Shell      string
	ShellSetup string
	Lines      []ShellScriptLine
	Excluded   []uint32
}
type ShellScriptInputBuilder func(context.Context, OccurrenceID, *string) (*ShellScriptInput, error)
