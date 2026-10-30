package action

import (
	"errors"
	"fmt"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// Decode populates action metadata from an owned YAML node graph.
func Decode(root *syntax.Node, metadata *Metadata) error {
	if metadata == nil {
		return errors.New("action metadata target must not be nil")
	}
	session, cursor, err := codec.Open(root)
	if err != nil {
		return err
	}
	defer session.Close()
	parser := manifestDecoder{session: session}
	entered, err := cursor.Enter()
	if err != nil {
		return session.Finish(err)
	}
	if !isNull(entered.Node()) {
		*metadata = Metadata{}
	}
	decodeErr := parser.metadata(entered, metadata)
	if !isNull(entered.Node()) {
		metadata.Root = entered.Node()
	}
	metadata.positions = &metadataPositionCache{}
	return session.Finish(decodeErr)
}

type manifestDecoder struct{ session *codec.Session }

func (d manifestDecoder) metadata(root codec.Entered, out *Metadata) error {
	n := root.Node()
	if isNull(n) {
		return nil
	}
	if n.Kind != syntax.MappingNode {
		return d.session.Wrap(n, expectedMapping("action metadata", n))
	}
	err := d.fields(root, "action metadata", map[string]func(codec.Cursor) error{
		"name":        func(cursor codec.Cursor) error { return decodeString(cursor, &out.Name) },
		"description": func(cursor codec.Cursor) error { return decodeString(cursor, &out.Description) },
		"inputs": func(cursor codec.Cursor) error {
			v, err := cursor.Enter()
			if err != nil {
				return err
			}
			out.Inputs, out.InputDefaults, err = d.inputs(v)
			return err
		},
		"outputs": func(cursor codec.Cursor) error {
			v, err := cursor.Enter()
			if err != nil {
				return err
			}
			out.Outputs, err = d.outputs(v)
			return err
		},
		"runs": func(cursor codec.Cursor) error {
			v, err := cursor.Enter()
			if err != nil {
				return err
			}
			out.Runs, err = d.runs(v)
			return err
		},
		"branding": func(cursor codec.Cursor) error {
			v, err := cursor.Enter()
			if err != nil {
				return err
			}
			out.Branding, err = d.branding(v)
			return err
		},
	})
	for _, value := range []*string{&out.Name, &out.Description, &out.Runs.Using, &out.Runs.Plugin,
		&out.Runs.Main, &out.Runs.Pre, &out.Runs.Post, &out.Runs.PreIf, &out.Runs.PostIf,
		&out.Runs.Image, &out.Runs.Entrypoint, &out.Runs.PreEntrypoint, &out.Runs.PostEntrypoint} {
		if literal := exprlang.LiteralExpressionValue(*value); literal != nil {
			*value = *literal
		}
	}
	return err
}

func (d manifestDecoder) fields(root codec.Entered, where string, handlers map[string]func(codec.Cursor) error) error {
	var errs []error
	seen := map[string]bool{}
	for key, value := range root.Pairs() {
		kn, err := key.Enter()
		if err != nil {
			return err
		}
		name := strings.ToLower(Key(kn.Node()))
		if seen[name] {
			errs = append(errs, d.session.Wrap(kn.Node(), codec.NewLoadError(fmt.Sprintf("duplicate %s key %q", where, Key(kn.Node())), codec.Mark{Line: kn.Node().Line, Column: kn.Node().Column})))
			continue
		}
		seen[name] = true
		if handler := handlers[name]; handler != nil {
			if err := handler(value); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (d manifestDecoder) inputs(root codec.Entered) (Inputs, []*KeyValue, error) {
	n := root.Node()
	if isNull(n) {
		return nil, nil, nil
	}
	if n.Kind != syntax.MappingNode {
		return nil, nil, d.session.Wrap(n, expectedMapping("inputs", n))
	}
	md := make(Inputs, len(n.Content)/2)
	var defaults []*KeyValue
	var sectionErr, softErr error
	for key, value := range root.Pairs() {
		kn, err := key.Enter()
		if err != nil {
			return nil, defaults, err
		}
		name := Key(kn.Node())
		id := strings.ToLower(name)
		definition, err := value.Enter()
		if err != nil {
			return nil, defaults, err
		}
		if isNull(definition.Node()) {
			if sectionErr == nil {
				if _, exists := md[id]; exists {
					sectionErr = d.session.Wrap(n, fmt.Errorf("input %q is duplicated", name))
				} else {
					md[id] = &Input{Name: name}
				}
			}
			continue
		}
		if definition.Node().Kind != syntax.MappingNode {
			if sectionErr == nil {
				sectionErr = d.session.Wrap(n, expectedMapping("input definition", definition.Node()))
			}
			continue
		}
		var required bool
		var defaultValue *string
		var deprecation string
		var deprecated bool
		var fieldErrors []error
		var notice error
		seen := map[string]bool{}
		for fieldKey, fieldValue := range definition.Pairs() {
			fkn, err := fieldKey.Enter()
			if err != nil {
				return nil, defaults, err
			}
			field := strings.ToLower(Key(fkn.Node()))
			var defaultNode codec.Entered
			if field == "default" {
				defaultNode, err = fieldValue.Enter()
				if err != nil {
					return nil, defaults, err
				}
				if str := yamlExprString(defaultNode.Node()); str != nil {
					defaults = append(defaults, &KeyValue{Name: name, Value: *str})
				}
			}
			if sectionErr != nil {
				continue
			}
			if seen[field] {
				fieldErrors = append(fieldErrors, codec.NewLoadError(
					fmt.Sprintf("duplicate input definition key %q", Key(fkn.Node())),
					codec.Mark{Line: fieldKey.Node().Line, Column: fieldKey.Node().Column},
				))
				continue
			}
			seen[field] = true
			switch field {
			case "required":
				err = fieldValue.Decode(&required)
			case "default":
				err = defaultNode.Decode(&defaultValue)
			case "deprecationmessage":
				deprecated = true
				err = fieldValue.Decode(&deprecation)
			case "description":
			default:
				notice = fmt.Errorf("unexpected key %q for definition of input %q", fieldKey.Node().Value, name)
			}
			if err != nil {
				if _, fatal := codec.FatalLoadError(err); fatal {
					return nil, defaults, err
				}
				fieldErrors = append(fieldErrors, err)
			}
		}
		if sectionErr != nil {
			continue
		}
		if len(fieldErrors) != 0 {
			sectionErr = d.session.Wrap(n, errors.Join(fieldErrors...))
			continue
		}
		if _, exists := md[id]; exists {
			sectionErr = d.session.Wrap(n, fmt.Errorf("input %q is duplicated", name))
			continue
		}
		md[id] = &Input{name, required && defaultValue == nil, deprecated, strings.TrimSpace(deprecation)}
		if notice != nil {
			softErr = notice
		}
	}
	if sectionErr != nil {
		return nil, defaults, sectionErr
	}
	return md, defaults, d.session.Wrap(n, softErr)
}

func (d manifestDecoder) outputs(root codec.Entered) (Outputs, error) {
	n := root.Node()
	if isNull(n) {
		return nil, nil
	}
	if n.Kind != syntax.MappingNode {
		return nil, d.session.Wrap(n, expectedMapping("outputs", n))
	}
	md := make(Outputs, len(n.Content)/2)
	for key := range root.Pairs() {
		kn, err := key.Enter()
		if err != nil {
			return nil, err
		}
		name := Key(kn.Node())
		id := strings.ToLower(name)
		if _, exists := md[id]; exists {
			return nil, d.session.Wrap(n, fmt.Errorf("output %q is duplicated", name))
		}
		md[id] = &Output{name}
	}
	return md, nil
}

func (d manifestDecoder) runs(root codec.Entered) (Runs, error) {
	n := root.Node()
	if isNull(n) {
		return Runs{}, nil
	}
	if n.Kind != syntax.MappingNode {
		return Runs{}, d.session.Wrap(n, expectedMapping("runs", n))
	}
	var out Runs
	handlers := map[string]func(codec.Cursor) error{}
	for name, target := range map[string]*string{"plugin": &out.Plugin, "using": &out.Using, "main": &out.Main, "pre": &out.Pre, "pre-if": &out.PreIf, "post": &out.Post, "post-if": &out.PostIf, "image": &out.Image, "pre-entrypoint": &out.PreEntrypoint, "entrypoint": &out.Entrypoint, "post-entrypoint": &out.PostEntrypoint} {
		handlers[name] = func(cursor codec.Cursor) error { return decodeString(cursor, target) }
	}
	handlers["steps"] = func(cursor codec.Cursor) error {
		v, err := cursor.Enter()
		if err != nil {
			return err
		}
		out.Steps, err = d.steps(v)
		return err
	}
	handlers["args"] = func(cursor codec.Cursor) error {
		return cursor.Decode(&out.Args)
	}
	handlers["env"] = func(cursor codec.Cursor) error {
		return cursor.Decode(&out.Env)
	}
	err := d.fields(root, "runs", handlers)
	return out, err
}

func decodeString(cursor codec.Cursor, dst *string) error {
	return cursor.Decode(dst)
}

func (d manifestDecoder) branding(root codec.Entered) (Branding, error) {
	n := root.Node()
	if isNull(n) {
		return Branding{}, nil
	}
	if n.Kind != syntax.MappingNode {
		return Branding{}, d.session.Wrap(n, expectedMapping("branding", n))
	}
	var out Branding
	handlers := map[string]func(codec.Cursor) error{
		"icon":  func(cursor codec.Cursor) error { return decodeString(cursor, &out.Icon) },
		"color": func(cursor codec.Cursor) error { return decodeString(cursor, &out.Color) },
	}
	err := d.fields(root, "branding", handlers)
	return out, err
}

func (d manifestDecoder) steps(root codec.Entered) (actionCompositeSteps, error) {
	n := root.Node()
	if isNull(n) {
		return nil, nil
	}
	if n.Kind != syntax.SequenceNode {
		return nil, d.session.Wrap(n, fmt.Errorf("yaml: steps must be sequence node but %s node was found at line:%d, col:%d", workflowparser.NodeKindName(n.Kind), n.Line, n.Column))
	}
	steps := make(actionCompositeSteps, 0, len(n.Content))
	for item := range root.Items() {
		entered, err := item.Enter()
		if err != nil {
			return steps, err
		}
		step, err := d.step(entered)
		if err != nil {
			return steps, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func (d manifestDecoder) step(root codec.Entered) (*Step, error) {
	n := root.Node()
	s := &Step{Line: n.Line, Column: n.Column, Node: n}
	if n.Kind != syntax.MappingNode {
		return s, nil
	}
	s.IsMapping = true
	s.Keys = make([]string, 0, len(n.Content)/2)
	for key, value := range root.Pairs() {
		kn, err := key.Enter()
		if err != nil {
			return s, err
		}
		keyName := Key(kn.Node())
		s.Keys = append(s.Keys, keyName)
		switch strings.ToLower(keyName) {
		case "id", "if", "run", "working-directory", "name", "with", "env", "shell", "continue-on-error", "uses":
			v, err := value.Enter()
			if err != nil {
				return s, err
			}
			vn := v.Node()
			switch strings.ToLower(keyName) {
			case "id":
				s.ID = yamlActionExprString(vn)
				if s.ID != nil {
					if lit := exprlang.LiteralExpressionValue(s.ID.Value); lit != nil {
						s.ID.Value = *lit
					}
				}
			case "if":
				s.If = yamlActionExprString(vn)
			case "run":
				s.Run = yamlActionExprString(vn)
			case "working-directory":
				s.WorkingDirectory = yamlActionExprString(vn)
			case "name":
				s.StepName = yamlActionExprString(vn)
			case "shell":
				s.Shell = yamlActionExprString(vn)
			case "continue-on-error":
				s.ContinueOnError = yamlExprString(vn)
			case "uses":
				s.Uses = Scalar(vn)
			case "with":
				s.With, s.WithExpression, err = d.keyValues(v)
				if err != nil {
					return s, err
				}
			case "env":
				s.Env, s.EnvExpression, err = d.keyValues(v)
				if err != nil {
					return s, err
				}
			}
		}
	}
	return s, nil
}

func (d manifestDecoder) keyValues(root codec.Entered) ([]*KeyValue, *ExprString, error) {
	n := root.Node()
	expression := yamlExprString(n)
	if n.Kind != syntax.MappingNode {
		return nil, expression, nil
	}
	var out []*KeyValue
	for key, value := range root.Pairs() {
		kn, err := key.Enter()
		if err != nil {
			return nil, expression, err
		}
		v, err := value.Enter()
		if err != nil {
			return nil, expression, err
		}
		if text := yamlExprString(v.Node()); text != nil {
			out = append(out, &KeyValue{Name: Key(kn.Node()), Value: *text})
		}
	}
	return out, expression, nil
}

func isNull(n *syntax.Node) bool { return n != nil && n.Kind == syntax.ScalarNode && n.Tag == "!!null" }
