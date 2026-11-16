package correctness

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"slices"
	"strconv"
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/contexts/events"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results/messages"
	"github.com/robfig/cron/v3"
)

// EventsRule is the single definition of the events rule.
var EventsRule = &define.Rule{
	Name:        "events",
	Description: "Validate workflow triggers and their options",
	Category:    "correctness",
	Subcategory: "triggers",
	Recommended: true,
	Help:        "Check the trigger name, activity type and filters at the reported location.",
	Checks:      define.Fixed(define.Checks{Workflow: Events}),
}

type eventsCheck struct{ reporting.Collector }

// Events validates event declarations.
func Events(n declaration.Workflow) []reporting.StepFinding {
	rule := &eventsCheck{}

	for _, e := range n.On {
		rule.checkEvent(e)
	}
	return rule.Findings
}

func (rule *eventsCheck) checkEvent(event workflowlang.Event) {
	if e, ok := event.(*workflowlang.ScheduledEvent); ok {
		for _, s := range e.Schedules {
			rule.checkCron(s.Cron)
			if s.Timezone != nil {
				rule.checkTimezone(s.Timezone)
			}
		}
	} else if e, ok := event.(*workflowlang.WorkflowDispatchEvent); ok {
		rule.checkWorkflowDispatchEvent(e)
	} else if _, ok := event.(*workflowlang.RepositoryDispatchEvent); ok {
		return
	} else if e, ok := event.(*workflowlang.WorkflowCallEvent); ok {
		rule.checkWorkflowCallEvent(e)
	} else if e, ok := event.(*workflowlang.WebhookEvent); ok {
		rule.checkWebhookEvent(e)
	} else if e, ok := event.(*workflowlang.ImageVersionEvent); ok {
		rule.checkImageVersionEvent(e)
	} else {
		panic("unreachable")
	}
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#onschedule
func (rule *eventsCheck) checkCron(spec *workflowlang.String) {
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := p.Parse(spec.Value)
	if err != nil {
		rule.Errorf(spec.Pos, "invalid CRON format %q in schedule event: %s", spec.Value, err.Error())
		return
	}

	start := sched.Next(time.Unix(0, 0))
	next := sched.Next(start)
	diff := next.Sub(start).Seconds()

	// (#14) https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#scheduled-events
	//
	// > The shortest interval you can run scheduled workflows is once every 5 minutes.
	if diff < 60.0*5 {
		rule.Errorf(spec.Pos, "scheduled job runs too frequently. it runs once per %g seconds. the shortest interval is once every 5 minutes", diff)
	}
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onschedule
func (rule *eventsCheck) checkTimezone(tz *workflowlang.String) {
	// Local is a Go host-timezone selector; UTC is a valid IANA alias for Etc/UTC.
	ok := tz.Value != "" && !strings.EqualFold(tz.Value, "local")
	if ok {
		_, err := time.LoadLocation(tz.Value)
		ok = err == nil
	}
	if !ok {
		rule.Errorf(tz.Pos, "invalid timezone %q in schedule event. it must be a valid IANA timezone name", tz.Value)
	}
}

func (rule *eventsCheck) filterNotAvailable(pos *sourcepos.Position, filter, hook string, available []string) {
	e := "events"
	if len(available) < 2 {
		e = "event"
	}
	rule.Errorf(pos, "%q filter is not available for %s event. it is only for %s %s", filter, hook, strings.Join(available, ", "), e)
}

func (rule *eventsCheck) checkExclusiveFilters(filter, ignore *workflowlang.WebhookEventFilter, hook string, available []string) {
	ok := slices.Contains(available, hook)

	if ok {
		if !filter.IsEmpty() && !ignore.IsEmpty() {
			p := sourcepos.NilUnlessPresent(filter.Name.Position())
			if q := sourcepos.NilUnlessPresent(ignore.Name.Position()); p.IsBefore(*q) {
				p = q
			}
			rule.Errorf(p, "both %q and %q filters cannot be used for the same event %q. note: use '!' to negate patterns", filter.Name.Text(), ignore.Name.Text(), hook)
		}
	} else {
		if !filter.IsEmpty() {
			rule.filterNotAvailable(sourcepos.NilUnlessPresent(filter.Name.Position()), filter.Name.Text(), hook, available)
		}
		if !ignore.IsEmpty() {
			rule.filterNotAvailable(sourcepos.NilUnlessPresent(ignore.Name.Position()), ignore.Name.Text(), hook, available)
		}
	}
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#webhook-events
func (rule *eventsCheck) checkWebhookEvent(event *workflowlang.WebhookEvent) {
	hook := event.Hook.Text()

	types, ok := events.AllWebhookTypes[hook]
	if !ok {
		rule.Errorf(event.Pos, "unknown Webhook event %q. see https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#webhook-events for list of all Webhook event names", hook)
		return
	}

	rule.checkTypes(event.Hook.Text(), sourcepos.NilUnlessPresent(event.Hook.Position()), event.Types, types)

	if hook == "workflow_run" {
		if len(event.Workflows) == 0 {
			rule.Error(event.Pos, "no workflow is configured for \"workflow_run\" event")
		}
	} else {
		if len(event.Workflows) != 0 {
			rule.Errorf(event.Pos, "\"workflows\" cannot be configured for %q event. it is only for workflow_run event", hook)
		}
	}

	// Some filters are available with specific events and exclusive
	// - on.merge_group.<branches|branches-ignore>
	// - on.<push|pull_request|pull_request_target>.<paths|paths-ignore>
	// - on.push.<branches|tags|branches-ignore|tags-ignore>
	// - on.<pull_request|pull_request_target>.<branches|branches-ignore>
	// - on.workflow_run.<branches|branches-ignore>
	rule.checkExclusiveFilters(
		event.Paths,
		event.PathsIgnore,
		hook,
		events.EventsWithField("paths"),
	)
	rule.checkExclusiveFilters(
		event.Branches,
		event.BranchesIgnore,
		hook,
		events.EventsWithField("branches"),
	)
	rule.checkExclusiveFilters(
		event.Tags,
		event.TagsIgnore,
		hook,
		events.EventsWithField("tags"),
	)
}

func (rule *eventsCheck) checkTypes(hook string, pos *sourcepos.Position, types []*workflowlang.String, expected []string) {
	if len(expected) == 0 && len(types) > 0 {
		rule.Errorf(pos, "\"types\" cannot be specified for %q Webhook event", hook)
		return
	}

	for _, ty := range types {
		valid := slices.Contains(expected, ty.Value)
		if !valid {
			rule.Errorf(
				ty.Pos,
				"invalid activity type %q for %q Webhook event. available types are %s",
				ty.Value,
				hook,
				messages.SortedQuotes(expected),
			)
		}
	}
}

// https://docs.github.com/en/actions/learn-github-actions/reusing-workflows
func (rule *eventsCheck) checkWorkflowCallEvent(event *workflowlang.WorkflowCallEvent) {
	for _, i := range event.Inputs {
		if i.Default == nil {
			continue
		}
		// ${{ }} is available in the default value
		if !i.Default.ContainsExpression() {
			switch i.Type {
			case workflowlang.WorkflowCallEventInputTypeNumber:
				if _, err := strconv.ParseFloat(i.Default.Text(), 64); err != nil {
					rule.Errorf(
						sourcepos.NilUnlessPresent(i.Default.Position()),
						"input of workflow_call event %q is typed as number but its default value %q cannot be parsed as a float number: %s",
						i.Name.Text(),
						i.Default.Text(),
						err,
					)
				}
			case workflowlang.WorkflowCallEventInputTypeBoolean:
				if d := strings.ToLower(i.Default.Text()); d != "true" && d != "false" {
					rule.Errorf(
						sourcepos.NilUnlessPresent(i.Default.Position()),
						"input of workflow_call event %q is typed as boolean. its default value must be true or false but got %q",
						i.Name.Text(),
						i.Default.Text(),
					)
				}
			case workflowlang.WorkflowCallEventInputTypeInvalid, workflowlang.WorkflowCallEventInputTypeString:
			}
		}
		if i.Required != nil && i.Required.Value {
			rule.Errorf(
				sourcepos.NilUnlessPresent(i.Default.Position()),
				"input %q of workflow_call event has the default value %q, but it is also required. if an input is marked as required, its default value will never be used",
				i.Name.Text(),
				i.Default.Text(),
			)
		}
	}
}

// https://github.blog/changelog/2021-11-10-github-actions-input-types-for-manual-workflows/
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#onworkflow_dispatchinputs
func (rule *eventsCheck) checkWorkflowDispatchEvent(event *workflowlang.WorkflowDispatchEvent) {
	for n, i := range event.Inputs {
		if i.Type == workflowlang.WorkflowDispatchEventInputTypeChoice {
			if len(i.Options) == 0 {
				rule.Errorf(sourcepos.NilUnlessPresent(i.Name.Position()), "input type of %q is \"choice\" but \"options\" is not set", n)
				continue
			}
			seen := make(map[string]struct{}, len(i.Options))
			for _, o := range i.Options {
				if _, ok := seen[o.Value]; ok {
					rule.Errorf(o.Pos, "option %q is duplicated in options of %q input", o.Value, n)
					continue
				}
				seen[o.Value] = struct{}{}
			}
			if i.Default != nil {
				var b messages.QuotesBuilder
				for _, o := range i.Options {
					b.Append(o.Value)
				}
				if _, ok := seen[i.Default.Text()]; !ok {
					rule.Errorf(sourcepos.NilUnlessPresent(i.Default.Position()), "default value %q of %q input is not included in its options %q", i.Default.Text(), n, b.Build())
				}
			}
		} else {
			if len(i.Options) > 0 {
				rule.Errorf(sourcepos.NilUnlessPresent(i.Name.Position()), "\"options\" can not be set to %q input because its input type is not \"choice\"", n)
			}
			if i.Default != nil {
				// TODO: Can some check be done for WorkflowDispatchEventInputTypeEnvironment?
				// What is suitable for default value of the type? (Or is a default value never suitable?)
				switch i.Type {
				case workflowlang.WorkflowDispatchEventInputTypeNumber:
					if _, err := strconv.ParseFloat(i.Default.Text(), 64); err != nil {
						rule.Errorf(
							sourcepos.NilUnlessPresent(i.Default.Position()),
							"type of %q input is \"number\" but its default value %q cannot be parsed as a float number: %s",
							i.Name.Text(),
							i.Default.Text(),
							err,
						)
					}
				case workflowlang.WorkflowDispatchEventInputTypeBoolean:
					if d := strings.ToLower(i.Default.Text()); d != "true" && d != "false" {
						rule.Errorf(sourcepos.NilUnlessPresent(i.Default.Position()), "type of %q input is \"boolean\". its default value %q must be \"true\" or \"false\"", n, i.Default.Text())
					}
				case workflowlang.WorkflowDispatchEventInputTypeNone,
					workflowlang.WorkflowDispatchEventInputTypeString,
					workflowlang.WorkflowDispatchEventInputTypeChoice,
					workflowlang.WorkflowDispatchEventInputTypeEnvironment:
				}
			}
		}
	}
	// Maximum number of inputs is 25
	// https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#providing-inputs
	// https://github.blog/changelog/2025-12-04-actions-workflow-dispatch-workflows-now-support-25-inputs
	if len(event.Inputs) > 25 {
		rule.Errorf(
			event.Pos,
			"maximum number of inputs for \"workflow_dispatch\" event is 25 but %d inputs are provided. see https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#providing-inputs",
			len(event.Inputs),
		)
	}
}

// https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#image_version
func (rule *eventsCheck) checkImageVersionEvent(event *workflowlang.ImageVersionEvent) {
	rule.checkTypes("image_version", event.Pos, event.Types, events.AllWebhookTypes["image_version"])
}
