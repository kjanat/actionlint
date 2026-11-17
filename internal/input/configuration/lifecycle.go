package configuration

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configdecode"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/services"
	"actionlint.kjanat.dev/v2/internal/model/severity"
)

type LifecycleConfig struct {
	Policies []LifecyclePolicy `yaml:"policies" jsonschema:"nullable,description=Named lifecycle notification policies." merge:"key=id"`
}
type LifecyclePolicy struct {
	ID            string                  `yaml:"id" jsonschema:"required,minLength=1,description=Stable policy identifier."`
	Description   string                  `yaml:"description" jsonschema:"required,minLength=1,description=Explains the policy purpose."`
	Match         LifecycleMatch          `yaml:"match" jsonschema:"required,description=Selects events covered by the policy."`
	Notifications []LifecycleNotification `yaml:"notifications" jsonschema:"required,minItems=1,description=Notification stages for matching events."`
}
type LifecycleMatch struct {
	Subjects []lifecycle.Subject    `yaml:"subjects" jsonschema:"required,minItems=1,description=Subject kinds matched by this policy."`
	Events   []lifecycle.EventKind  `yaml:"events" jsonschema:"required,minItems=1,description=Event kinds matched by this policy."`
	Services []services.ServiceKind `yaml:"services,omitempty" jsonschema:"description=Services matched by this policy. Omit to match all services."`
}
type LifecycleNotification struct {
	Starts   LifecycleStart    `yaml:"starts" jsonschema:"required,description=Date at which this notification begins."`
	Severity severity.Severity `yaml:"severity" jsonschema:"required,enum=info,enum=warning,enum=error,description=Severity of this notification stage."`
}
type LifecycleStart struct {
	Before string `yaml:"before,omitempty" jsonschema:"pattern=^[0-9]+d$,description=Calendar days before the effective date."`
	On     string `yaml:"on,omitempty" jsonschema:"enum=effective-date,description=Start on the effective date."`
}

// DecodeConfig requires exactly one notification start form.
func (s *LifecycleStart) DecodeConfig(value any) error {
	object, ok := value.(map[string]any)
	if !ok || len(object) != 1 {
		return errors.New("starts must contain exactly one of before or on")
	}
	type plain LifecycleStart
	var next plain
	if err := configdecode.Decode(value, &next); err != nil {
		return err
	}
	*s = LifecycleStart(next)
	return nil
}

// Compile validates the complete policy list and returns immutable domain policies.
func (c LifecycleConfig) Compile(origin string) ([]lifecycle.Policy, error) {
	return c.compile(origin, nil, "")
}

func (c LifecycleConfig) compile(origin string, inputs map[string]document.Location, prefix string) ([]lifecycle.Policy, error) {
	out := make([]lifecycle.Policy, 0, len(c.Policies))
	ids := map[string]bool{}
	for i, p := range c.Policies {
		if ids[p.ID] {
			return nil, fmt.Errorf("policies[%d]: duplicate id %q", i, p.ID)
		}
		ids[p.ID] = true
		ns := make([]lifecycle.Notification, 0, len(p.Notifications))
		for j, n := range p.Notifications {
			var start lifecycle.Start
			switch {
			case n.Starts.On == "effective-date" && n.Starts.Before == "":
				start = lifecycle.OnEffective()
			case n.Starts.On == "" && strings.HasSuffix(n.Starts.Before, "d"):
				raw := strings.TrimSuffix(n.Starts.Before, "d")
				days, err := strconv.ParseUint(raw, 10, 32)
				if err != nil || raw == "" || days > 365000 {
					return nil, fmt.Errorf("policies[%d].notifications[%d]: invalid before duration", i, j)
				}
				start = lifecycle.Before(uint32(days))
			default:
				return nil, fmt.Errorf("policies[%d].notifications[%d]: starts must contain exactly one valid before or on", i, j)
			}
			ns = append(ns, lifecycle.Notification{Starts: start, Severity: n.Severity})
		}
		source := inputs[fmt.Sprintf("%s/%d/id", prefix, i)].File
		if source == "" {
			source = origin
		}
		policy, err := lifecycle.NewPolicy(p.ID, p.Description, lifecycle.Match{Subjects: p.Match.Subjects, Events: p.Match.Events, Services: p.Match.Services}, ns, source)
		if err != nil {
			return nil, fmt.Errorf("policies[%d]: %w", i, err)
		}
		out = append(out, policy)
	}
	return out, nil
}

func (c *LifecycleConfig) DecodeConfig(value any) error {
	type plain LifecycleConfig
	var next plain
	if err := configdecode.Decode(value, &next); err != nil {
		return err
	}
	if _, err := LifecycleConfig(next).Compile(""); err != nil {
		return err
	}
	*c = LifecycleConfig(next)
	return nil
}

func (cfg *Config) LifecyclePolicies() ([]lifecycle.Policy, error) {
	if cfg == nil {
		return nil, nil
	}
	return cfg.Lint.Lifecycle.compile("", cfg.Lint.locations, "/lint/lifecycle/policies")
}
