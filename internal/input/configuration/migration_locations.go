package config

import (
	"maps"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

// migrationInputs associates moved settings with their original source text.
func (ruleSet RuleSet) migrationInputs(value any, inputs map[string]Input) map[string]Input {
	next := maps.Clone(inputs)
	if next == nil {
		next = map[string]Input{}
	}
	move := func(from, to string) {
		for path, origin := range inputs {
			if path == from || strings.HasPrefix(path, from+"/") {
				next[to+strings.TrimPrefix(path, from)] = origin
			}
		}
	}
	var visit func(any, string)
	visit = func(value any, prefix string) {
		at := func(key string) string { return document.Path(prefix, key) }
		for _, pair := range [][2]string{{"self-hosted-runner/labels", "runners"}, {"config-variables", "variables"}, {"config-secrets", "secrets"}, {"assume-default-permissions", "default-workflow-permissions"}} {
			move(prefix+"/"+pair[0], at("repository")+"/"+pair[1])
		}
		for name, selected := range members(field(value, "policy")) {
			id := ruleSet.ResolveAlias(migratedPermissionsName(name))
			declaration, found := ruleSet.FindRule(id)
			if !found {
				continue
			}
			from := document.Path(at("policy"), name)
			to := document.Path(document.Path(at("lint")+"/rules", declaration.Category), id)
			move(from, to)
			move(from, to+"/level")
			if isMapping(selected) {
				move(from, to+"/options")
			}
			if id == "missing-permissions" {
				move(from+"/scope", to+"/options/require-at")
			}
		}
		for pattern := range members(field(value, "paths")) {
			move(document.Path(at("paths"), pattern)+"/ignore", document.Path(at("lint")+"/message-ignores", pattern))
		}
		for category, group := range members(field(field(value, "lint"), "rules")) {
			for name, selected := range members(group) {
				id := ruleSet.ResolveAlias(migratedPermissionsName(name))
				from := document.Path(document.Path(at("lint")+"/rules", category), name)
				to := document.Path(document.Path(at("lint")+"/rules", category), id)
				move(from, to)
				if declaration, found := ruleSet.FindRule(id); found && declaration.Category != category {
					to = document.Path(document.Path(at("lint")+"/rules", declaration.Category), id)
					move(from, to)
				}
				if id == "missing-permissions" && name != id {
					move(from+"/options/scope", to+"/options/require-at")
				}
				for old, target := range map[string]string{"runner-label": "runners", "workflow-call": "default-workflow-permissions"} {
					if id == old {
						key := "labels"
						if old == "workflow-call" {
							key = "default-permissions"
						}
						move(from+"/options/"+key, document.Path(at("repository"), target))
					}
				}
				if id == "expression-reference" {
					for _, key := range []string{"variables", "secrets"} {
						move(from+"/options/"+key, document.Path(at("repository"), key))
					}
				}
				if !isMapping(selected) {
					move(from, to+"/level")
				}
			}
		}
		for name, selected := range members(field(value, "tools")) {
			base := document.Path(at("tools"), name)
			if isBool(selected) {
				move(base, base+"/enabled")
			}
			for key, shorthand := range toolProviders()[name].shorthands {
				target := base + "/config"
				for _, part := range shorthand.Path {
					target = document.Path(target, part)
				}
				move(document.Path(base, key), target)
			}
		}
		for i, child := range elements(field(value, "overrides")) {
			visit(child, document.Index(at("overrides"), i))
		}
	}
	visit(value, "")
	return next
}
