package actionlint

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

type actionMetadataFinding struct {
	key   actionMetadataFindingKey
	visit uint64
}

func newActionMetadataValidation() *actionMetadataValidation {
	return &actionMetadataValidation{templates: make(map[string][]*Error)}
}

func (rule *RuleAction) replayMetadataFindings(meta *ActionMetadata, action *ExecAction) {
	store := rule.validation
	path := meta.Path()
	templates, checked := store.templates[path]
	if !checked {
		validator := NewRuleAction(rule.cache)
		validator.SetConfig(rule.Config())
		validator.checkLocalActionMetadata(meta, action)
		templates = validator.Errs()
		store.templates[path] = templates
	}
	store.visits++
	for _, template := range templates {
		finding := *template
		key := actionMetadataFindingKey{path: path, message: finding.Message}
		if finding.source == nil {
			finding.Line, finding.Column = action.Uses.Pos.Line, action.Uses.Pos.Col
		} else {
			key.line, key.column = finding.Line, finding.Column
		}
		if finding.ignoreOrigin != nil {
			origin := *finding.ignoreOrigin
			finding.ignoreOrigin = &origin
		}
		if finding.endPosition != nil {
			end := *finding.endPosition
			finding.endPosition = &end
		}
		finding.metadataFinding = &actionMetadataFinding{key, store.visits}
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
		if identity := finding.metadataFinding; identity != nil && finding.Kind == "action" {
			k := key{identity.key, finding.Kind, finding.severity}
			if previous := first[k]; previous == nil || identity.visit < previous.metadataFinding.visit {
				first[k] = finding
			}
		}
	}
	filtered := findings[:0]
	for _, finding := range findings {
		if identity := finding.metadataFinding; identity != nil && finding.Kind == "action" && first[key{identity.key, finding.Kind, finding.severity}] != finding {
			continue
		}
		filtered = append(filtered, finding)
	}
	clear(findings[len(filtered):])
	return filtered
}
