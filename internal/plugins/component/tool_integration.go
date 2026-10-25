package component

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"github.com/kjanat/wacogo"
)

func (p *Plugin) ToolIntegration() *analyzer.ToolIntegration {
	if p.toolIntegration == nil {
		return nil
	}
	descriptor := *p.toolIntegration
	return &descriptor
}

func (p *Plugin) loadToolIntegration(ctx context.Context) error {
	value, err := p.callInterface(ctx, ToolIntegrationInterface, "describe")
	if err != nil {
		return err
	}
	record, err := liftRecord(value, "tool integration")
	if err != nil {
		return err
	}
	var descriptor analyzer.ToolIntegration
	for name, dest := range map[string]*string{"tool": &descriptor.Tool, "contract": &descriptor.Contract, "configuration-schema": &descriptor.ConfigurationSchema} {
		*dest, err = liftString(record, name)
		if err != nil {
			return err
		}
	}
	if descriptor.Tool == "" || strings.TrimSpace(descriptor.Tool) != descriptor.Tool {
		return errors.New("tool integration requires a nonempty tool identifier")
	}
	if p.analyzerDescriptor == nil || descriptor.Tool != p.analyzerDescriptor.Name {
		return errors.New("tool integration identifier must match analyzer rule identity")
	}
	if err := validateSuppressionRuleName(descriptor.Tool); err != nil {
		return err
	}
	if !strings.Contains(descriptor.Contract, "@") {
		return errors.New("tool integration requires a versioned contract identifier")
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal([]byte(descriptor.ConfigurationSchema), &schema); err != nil || schema == nil {
		return errors.New("tool integration configuration schema must be a JSON object")
	}
	if _, err := validOptionsSchema(descriptor.ConfigurationSchema); err != nil {
		return fmt.Errorf("tool integration configuration schema: %w", err)
	}
	p.toolIntegration = &descriptor
	return nil
}

func (p *Plugin) NegotiateTool(ctx context.Context, config, explicit json.RawMessage) (*analyzer.ToolNegotiation, error) {
	if p.toolIntegration == nil {
		return nil, errors.New("component does not implement tool integration")
	}
	options, err := scriptOptions(config)
	if err != nil {
		return nil, err
	}
	pluginOptions, err := scriptOptions(explicit)
	if err != nil {
		return nil, err
	}
	value, err := p.callInterface(ctx, ToolIntegrationInterface, "negotiate", wacogo.ValString(options), wacogo.ValString(pluginOptions))
	if err != nil {
		return nil, err
	}
	return liftToolNegotiation(value)
}

func liftToolNegotiation(value wacogo.Val) (*analyzer.ToolNegotiation, error) {
	result, ok := value.(*wacogo.ValResult)
	if !ok {
		return nil, liftError{"tool negotiation", value}
	}
	if !result.IsOk() {
		reason, ok := result.Err().(wacogo.ValString)
		if !ok {
			return nil, liftError{"unsupported configuration", result.Err()}
		}
		return nil, &analyzer.UnsupportedToolConfiguration{Reason: string(reason)}
	}
	record, err := liftRecord(result.Ok(), "tool negotiation")
	if err != nil {
		return nil, err
	}
	raw, err := liftString(record, "options")
	if err != nil {
		return nil, err
	}
	options, err := scriptOptions(json.RawMessage(raw))
	if err != nil {
		return nil, err
	}
	list, err := liftList(record.Field("resources"), "tool resources")
	if err != nil {
		return nil, err
	}
	negotiation := &analyzer.ToolNegotiation{Options: json.RawMessage(options)}
	for i := range list.Len() {
		variant, ok := list.Get(i).(*wacogo.ValVariant)
		if !ok {
			return nil, liftError{"tool resource", list.Get(i)}
		}
		switch variant.Discriminant() {
		case 0:
			negotiation.Resources = append(negotiation.Resources, analyzer.ToolResourceRequest{Kind: analyzer.ToolWorkspace})
		case 1:
			path, ok := variant.Val().(wacogo.ValString)
			if !ok || path == "" {
				return nil, liftError{"tool resource directory", variant.Val()}
			}
			negotiation.Resources = append(negotiation.Resources, analyzer.ToolResourceRequest{Kind: analyzer.ToolDirectory, Path: string(path)})
		default:
			return nil, liftError{"tool resource", variant}
		}
	}
	return negotiation, nil
}
