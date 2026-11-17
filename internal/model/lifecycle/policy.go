package lifecycle

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/internal/model/services"
)

type Subject string

const (
	Runners          Subject = "runners"
	ActionRuntimes   Subject = "action-runtimes"
	LanguageRuntimes Subject = "language-runtimes"
)

type EventKind string

const (
	SupportEnds     EventKind = "support-ends"
	SupportStarts   EventKind = "support-starts"
	RuntimeReplaced EventKind = "runtime-replaced"
)

type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

func (s Severity) Rank() int {
	switch s {
	case Info:
		return 1
	case Warning:
		return 2
	case Error:
		return 3
	}
	return 0
}

type Evidence struct {
	Subject                    Subject
	Name                       string
	Kind                       EventKind
	Service                    services.Service
	Announced, Effective, AsOf Date
	Source                     string
	Current                    bool
	AllOf, AnyOf               []Evidence
}
type Match struct {
	Subjects []Subject
	Events   []EventKind
	Services []services.ServiceKind
}
type Start struct {
	days      uint32
	effective bool
}

func Before(days uint32) Start { return Start{days: days} }
func OnEffective() Start       { return Start{effective: true} }

type Notification struct {
	Starts   Start
	Severity Severity
}
type Policy struct {
	id, description, origin string
	match                   Match
	notifications           []Notification
}

func NewPolicy(id, description string, match Match, notifications []Notification, origin string) (Policy, error) {
	if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) {
		return Policy{}, errors.New("policy id must be nonempty and have no surrounding whitespace")
	}
	if strings.TrimSpace(description) == "" {
		return Policy{}, errors.New("policy description must be nonempty")
	}
	if len(match.Subjects) == 0 || len(match.Events) == 0 || len(notifications) == 0 {
		return Policy{}, errors.New("subjects, events and notifications must be nonempty")
	}
	for _, s := range match.Subjects {
		if s != Runners && s != ActionRuntimes && s != LanguageRuntimes {
			return Policy{}, fmt.Errorf("unknown subject %q", s)
		}
	}
	for _, e := range match.Events {
		if e != SupportEnds && e != SupportStarts && e != RuntimeReplaced {
			return Policy{}, fmt.Errorf("unknown event %q", e)
		}
	}
	for _, s := range match.Services {
		if s != services.GitHubCom && s != services.GitHubDataResidency && s != services.GitHubEnterpriseServer {
			return Policy{}, fmt.Errorf("unknown service %q", s)
		}
	}
	seen := map[Start]bool{}
	for _, n := range notifications {
		if n.Severity.Rank() == 0 || n.Starts.days > 365000 {
			return Policy{}, errors.New("invalid notification")
		}
		if seen[n.Starts] {
			return Policy{}, errors.New("duplicate notification start")
		}
		seen[n.Starts] = true
	}
	match.Subjects = slices.Clone(match.Subjects)
	match.Events = slices.Clone(match.Events)
	match.Services = slices.Clone(match.Services)
	return Policy{id: id, description: description, origin: origin, match: match, notifications: slices.Clone(notifications)}, nil
}
func (p Policy) ID() string                    { return p.id }
func (p Policy) Description() string           { return p.description }
func (p Policy) Origin() string                { return p.origin }
func (p Policy) MatchesSubject(s Subject) bool { return slices.Contains(p.match.Subjects, s) }

type Decision struct {
	PolicyID, Description, Origin string
	Severity                      Severity
	StartsOn                      Date
}
type Assessment struct {
	Matched  bool
	Severity Severity
	Active   []Decision
}

func Assess(e Evidence, policies []Policy) Assessment {
	if len(e.AllOf) > 0 {
		combined := Assessment{Matched: true}
		allActive := true
		for _, child := range e.AllOf {
			assessed := Assess(child, policies)
			if !assessed.Matched {
				combined.Matched = false
			}
			if assessed.Severity.Rank() == 0 {
				allActive = false
				continue
			}
			if combined.Severity == "" || assessed.Severity.Rank() < combined.Severity.Rank() {
				combined.Severity = assessed.Severity
			}
			combined.Active = append(combined.Active, assessed.Active...)
		}
		if !allActive {
			combined.Severity = ""
			combined.Active = nil
		}
		normalizeDecisions(&combined)
		return combined
	}
	if len(e.AnyOf) > 0 {
		var combined Assessment
		for _, child := range e.AnyOf {
			assessed := Assess(child, policies)
			combined.Matched = combined.Matched || assessed.Matched
			rank := assessed.Severity.Rank()
			if rank == 0 || rank < combined.Severity.Rank() {
				continue
			}
			if rank > combined.Severity.Rank() {
				combined.Severity = assessed.Severity
				combined.Active = nil
			}
			combined.Active = append(combined.Active, assessed.Active...)
		}
		normalizeDecisions(&combined)
		return combined
	}
	var a Assessment
	best := 0
	for _, p := range policies {
		if !p.MatchesSubject(e.Subject) || !slices.Contains(p.match.Events, e.Kind) || len(p.match.Services) > 0 && !slices.Contains(p.match.Services, e.Service.Kind()) {
			continue
		}
		a.Matched = true
		if e.Effective.IsZero() || e.AsOf.IsZero() {
			continue
		}
		for _, n := range p.notifications {
			t, _ := time.Parse("2006-01-02", e.Effective.String())
			if !n.Starts.effective {
				t = t.AddDate(0, 0, -int(n.Starts.days))
				if t.Year() < 0 {
					t = time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)
				}
			}
			start := DateOf(t)
			if !e.Announced.IsZero() && start.Compare(e.Announced) < 0 {
				start = e.Announced
			}
			if e.AsOf.Compare(start) < 0 {
				continue
			}
			rank := n.Severity.Rank()
			if rank < best {
				continue
			}
			if rank > best {
				best = rank
				a.Active = nil
			}
			a.Severity = n.Severity
			a.Active = append(a.Active, Decision{p.id, p.description, p.origin, n.Severity, start})
		}
	}
	normalizeDecisions(&a)
	return a
}

func normalizeDecisions(a *Assessment) {
	slices.SortFunc(a.Active, func(a, b Decision) int {
		if n := strings.Compare(a.PolicyID, b.PolicyID); n != 0 {
			return n
		}
		if n := strings.Compare(a.Origin, b.Origin); n != 0 {
			return n
		}
		if n := a.StartsOn.Compare(b.StartsOn); n != 0 {
			return n
		}
		if n := a.Severity.Rank() - b.Severity.Rank(); n != 0 {
			return n
		}
		return strings.Compare(a.Description, b.Description)
	})
	a.Active = slices.Compact(a.Active)
}
