package engine

import (
	"strconv"
	"strings"

	actionmeta "actionlint.kjanat.dev/internal/action"
	actioncheck "actionlint.kjanat.dev/internal/action/validation"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/fsread"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// Each workflow analysis owns its validation store.
// Its templates contain no caller state and are also shared with nested composites.
type actionMetadataValidation struct {
	templates map[string][]*findingmodel.Finding
	visits    uint64
}

func metadataFindingKey(meta *actionmeta.Metadata, finding *findingmodel.Finding) findingmodel.MetadataKey {
	// Validation messages may embed the metadata filename or directory. Normalize
	// those spellings for identity without changing the user-facing source path.
	message := strings.NewReplacer(
		strconv.Quote(meta.Path()), strconv.Quote(fsread.PhysicalPath(meta.Path())),
		strconv.Quote(meta.Dir()), strconv.Quote(fsread.PhysicalPath(meta.Dir())),
	).Replace(finding.Message)
	key := findingmodel.MetadataKey{Path: fsread.PhysicalPath(meta.Path()), Message: message}
	if finding.Source != nil {
		key.Line, key.Column = finding.Start.Line, finding.Start.Column
	}
	return key
}

func newActionMetadataValidation() *actionMetadataValidation {
	return &actionMetadataValidation{templates: make(map[string][]*findingmodel.Finding)}
}

func (rule *RuleAction) replayMetadataFindings(meta *actionmeta.Metadata, action *workflowlang.ExecAction, displaySpec string) {
	store := rule.validation
	path := meta.Path()
	templates, checked := store.templates[path]
	if !checked {
		rule.Debug("Checking metadata of %s action %q at %q", meta.Runs.Using, meta.Name, displaySpec)
		templates = actioncheck.Check(meta, action.Uses.Pos)
		store.templates[path] = templates
	}
	store.visits++
	for _, template := range templates {
		finding := *template
		key := metadataFindingKey(meta, &finding)
		if finding.Source == nil {
			finding.Start.Line, finding.Start.Column = action.Uses.Pos.Line, action.Uses.Pos.Col
		}
		if finding.IgnoreOrigin != nil {
			origin := *finding.IgnoreOrigin
			finding.IgnoreOrigin = &origin
		}
		if finding.EndPosition != nil {
			end := *finding.EndPosition
			finding.EndPosition = &end
		}
		finding.MetadataIdentity = &findingmodel.MetadataIdentity{Key: key, Visit: store.visits}
		rule.Findings = append(rule.Findings, &finding)
	}
}
