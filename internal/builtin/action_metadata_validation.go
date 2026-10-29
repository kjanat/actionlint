package builtin

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// Each workflow analysis owns its validation store.
// Its templates contain no caller state and are also shared with nested composites.
type actionMetadataValidation struct {
	templates map[string][]*findingmodel.Finding
	visits    uint64
}

func metadataFindingKey(meta *actionmeta.Metadata, document string, finding *findingmodel.Finding) findingmodel.MetadataKey {
	// Validation messages may embed the metadata filename or directory. Normalize
	// those spellings for identity without changing the user-facing source path.
	message := strings.NewReplacer(
		strconv.Quote(meta.Path()), strconv.Quote(document),
		strconv.Quote(meta.Dir()), strconv.Quote(path.Dir(document)),
	).Replace(finding.Message)
	key := findingmodel.MetadataKey{Path: document, Message: message}
	if finding.Source != nil {
		key.Line, key.Column = finding.Start.Line, finding.Start.Column
	}
	return key
}

func newActionMetadataValidation() *actionMetadataValidation {
	return &actionMetadataValidation{templates: make(map[string][]*findingmodel.Finding)}
}

func (rule *RuleAction) replayMetadataFindings(meta *actionmeta.Metadata, source actionlocal.SourceDocument, action workflowdecl.ExecActionView, displaySpec string) error {
	store := rule.validation
	path := meta.Path()
	templates, checked := store.templates[path]
	if !checked {
		rule.Debug("Checking metadata of %s action %q at %q", meta.Runs.Using, meta.Name, displaySpec)
		templates = actioncheck.Check(meta, declarationPosition(action.Uses()))
		store.templates[path] = templates
	}
	if len(templates) != 0 {
		original, present := meta.OriginalSource()
		if !source.Capture().Valid() || !present || original != string(source.Content()) {
			return fmt.Errorf("metadata source %q has no matching captured revision", path)
		}
	}
	document := source.Capture().Revision().Document()
	store.visits++
	for _, template := range templates {
		finding := *template
		key := metadataFindingKey(meta, document, &finding)
		if finding.Source == nil {
			finding.Start.Line, finding.Start.Column = declarationPosition(action.Uses()).Line, declarationPosition(action.Uses()).Col
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
		rule.Report(&finding)
	}
	return nil
}
