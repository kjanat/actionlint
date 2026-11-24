package correctness

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/actioncheck"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"

	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"

	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// actionMetadataValidation templates hold no caller state and are shared with nested composites.
type actionMetadataValidation struct {
	templates map[string][]*finding.Finding
	copies    *finding.Copier
	visits    uint64
}

func metadataFindingKey(meta *actionmeta.Metadata, document string, item *finding.Finding) finding.MetadataKey {
	// Validation messages may embed the metadata filename or directory. Normalize
	// those spellings for identity without changing the user-facing source path.
	message := strings.NewReplacer(
		strconv.Quote(meta.Path()), strconv.Quote(document),
		strconv.Quote(meta.Dir()), strconv.Quote(path.Dir(document)),
	).Replace(item.Message)
	key := finding.MetadataKey{Path: document, Message: message}
	if item.Source.Content != nil {
		key.Start = item.Start
	}
	return key
}

func newActionMetadataValidation() *actionMetadataValidation {
	return &actionMetadataValidation{templates: make(map[string][]*finding.Finding), copies: finding.NewCopier()}
}

func (rule *ruleAction) replayMetadataFindings(meta *actionmeta.Metadata, source localactions.SourceDocument, action *definition.ExecAction, displaySpec string) error {
	store := rule.validation
	path := meta.Path()
	templates, checked := store.templates[path]
	if !checked {
		rule.Debug("Checking metadata of %s action %q at %q", meta.Runs.Using, meta.Name, displaySpec)
		templates = actioncheck.CheckWithAdvisories(meta, reporting.SourcePosition(action.Uses), source.ReferencedFiles(), rule.lifecycle)
		store.templates[path] = templates
	}
	if len(templates) != 0 {
		original, present := meta.Text()
		if !source.Capture().Valid() || !present || original != string(source.Content()) {
			return fmt.Errorf("metadata source %q has no matching captured revision", path)
		}
	}
	document := source.Capture().Revision().Document()
	store.visits++
	for _, template := range templates {
		item := store.copies.Clone(template)
		key := metadataFindingKey(meta, document, item)
		if item.Source.Content == nil {
			item.Start.Line, item.Start.Column = reporting.SourcePosition(action.Uses).Line, reporting.SourcePosition(action.Uses).Column
		}
		item.MetadataIdentity = &finding.MetadataIdentity{Key: key, Visit: store.visits}
		rule.Report(item)
	}
	return nil
}
