package engine

import (
	"strconv"
	"strings"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// Each workflow analysis owns its validation store.
// Its templates contain no caller state and are also shared with nested composites.
type actionMetadataValidation struct {
	templates map[string][]*Error
	visits    uint64
}

type actionMetadataFindingKey struct {
	path, message string
	line, column  int
}

func metadataFindingKey(meta *ActionMetadata, finding *Error) actionMetadataFindingKey {
	// Validation messages may embed the metadata filename or directory. Normalize
	// those spellings for identity without changing the user-facing source path.
	message := strings.NewReplacer(
		strconv.Quote(meta.Path()), strconv.Quote(physicalSourcePath(meta.Path())),
		strconv.Quote(meta.Dir()), strconv.Quote(physicalSourcePath(meta.Dir())),
	).Replace(finding.Message)
	key := actionMetadataFindingKey{path: physicalSourcePath(meta.Path()), message: message}
	if finding.source != nil {
		key.line, key.column = finding.Start.Line, finding.Start.Column
	}
	return key
}

type actionMetadataFinding struct {
	key         actionMetadataFindingKey
	visit       uint64
	declaration bool
}

func newActionMetadataValidation() *actionMetadataValidation {
	return &actionMetadataValidation{templates: make(map[string][]*Error)}
}

func (rule *RuleAction) replayMetadataFindings(meta *ActionMetadata, action *workflowlang.ExecAction, displaySpec string) {
	store := rule.validation
	path := meta.Path()
	templates, checked := store.templates[path]
	if !checked {
		rule.Debug("Checking metadata of %s action %q at %q", meta.Runs.Using, meta.Name, displaySpec)
		validator := NewRuleAction(rule.cache)
		validator.SetConfig(rule.Config())
		validator.EnableDebug(rule.dbg)
		validator.checkLocalActionMetadata(meta, action)
		templates = validator.Errs()
		store.templates[path] = templates
	}
	store.visits++
	for _, template := range templates {
		finding := *template
		key := metadataFindingKey(meta, &finding)
		if finding.source == nil {
			finding.Start.Line, finding.Start.Column = action.Uses.Pos.Line, action.Uses.Pos.Col
		}
		if finding.ignoreOrigin != nil {
			origin := *finding.ignoreOrigin
			finding.ignoreOrigin = &origin
		}
		if finding.endPosition != nil {
			end := *finding.endPosition
			finding.endPosition = &end
		}
		finding.metadataFinding = &actionMetadataFinding{key: key, visit: store.visits}
		rule.errs = append(rule.errs, &finding)
	}
}

// Filtering may reorder findings by source file. Preserve the first visible
// invocation independently of map iteration order.
// Policy diagnostics copied from a finding remain distinct from that finding.
func firstVisibleMetadataFindings(findings []*Error) []*Error {
	type key struct {
		finding        actionMetadataFindingKey
		kind, severity string
	}
	first := make(map[key]*Error)
	for _, finding := range findings {
		if identity := finding.metadataFinding; identity != nil && finding.Rule == "action" {
			k := key{identity.key, finding.Rule, finding.Severity}
			if previous := first[k]; previous == nil || identity.visit < previous.metadataFinding.visit {
				first[k] = finding
			}
		}
	}
	filtered := findings[:0]
	for _, finding := range findings {
		if identity := finding.metadataFinding; identity != nil && finding.Rule == "action" && first[key{identity.key, finding.Rule, finding.Severity}] != finding {
			continue
		}
		filtered = append(filtered, finding)
	}
	clear(findings[len(filtered):])
	return filtered
}
