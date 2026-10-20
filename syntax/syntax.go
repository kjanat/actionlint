// Package syntax exposes workflow syntax trees and parser diagnostics.
package syntax

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

type (
	Pos                            = workflowlang.Pos
	String                         = workflowlang.String
	Bool                           = workflowlang.Bool
	Int                            = workflowlang.Int
	Float                          = workflowlang.Float
	Event                          = workflowlang.Event
	WebhookEventFilter             = workflowlang.WebhookEventFilter
	WebhookEvent                   = workflowlang.WebhookEvent
	ScheduleEntry                  = workflowlang.ScheduleEntry
	ScheduledEvent                 = workflowlang.ScheduledEvent
	WorkflowDispatchEventInputType = workflowlang.WorkflowDispatchEventInputType
	DispatchInput                  = workflowlang.DispatchInput
	WorkflowDispatchEvent          = workflowlang.WorkflowDispatchEvent
	RepositoryDispatchEvent        = workflowlang.RepositoryDispatchEvent
	WorkflowCallEventInputType     = workflowlang.WorkflowCallEventInputType
	WorkflowCallEventInput         = workflowlang.WorkflowCallEventInput
	WorkflowCallEventSecret        = workflowlang.WorkflowCallEventSecret
	WorkflowCallEventOutput        = workflowlang.WorkflowCallEventOutput
	WorkflowCallEvent              = workflowlang.WorkflowCallEvent
	ImageVersionEvent              = workflowlang.ImageVersionEvent
	PermissionScope                = workflowlang.PermissionScope
	Permissions                    = workflowlang.Permissions
	DefaultsRun                    = workflowlang.DefaultsRun
	Defaults                       = workflowlang.Defaults
	Concurrency                    = workflowlang.Concurrency
	Environment                    = workflowlang.Environment
	ExecKind                       = workflowlang.ExecKind
	Exec                           = workflowlang.Exec
	ExecRun                        = workflowlang.ExecRun
	Input                          = workflowlang.Input
	ActionInputMapping             = workflowlang.ActionInputMapping
	ExecAction                     = workflowlang.ExecAction
	ExecWait                       = workflowlang.ExecWait
	ExecCancel                     = workflowlang.ExecCancel
	ExecParallel                   = workflowlang.ExecParallel
	RawYAMLValueKind               = workflowlang.RawYAMLValueKind
	RawYAMLValue                   = workflowlang.RawYAMLValue
	RawYAMLObject                  = workflowlang.RawYAMLObject
	RawYAMLArray                   = workflowlang.RawYAMLArray
	RawYAMLString                  = workflowlang.RawYAMLString
	MatrixRow                      = workflowlang.MatrixRow
	MatrixAssign                   = workflowlang.MatrixAssign
	MatrixCombination              = workflowlang.MatrixCombination
	MatrixCombinations             = workflowlang.MatrixCombinations
	Matrix                         = workflowlang.Matrix
	Strategy                       = workflowlang.Strategy
	EnvVar                         = workflowlang.EnvVar
	Env                            = workflowlang.Env
	Step                           = workflowlang.Step
	Credentials                    = workflowlang.Credentials
	Container                      = workflowlang.Container
	Service                        = workflowlang.Service
	Services                       = workflowlang.Services
	Output                         = workflowlang.Output
	Runner                         = workflowlang.Runner
	WorkflowCallInput              = workflowlang.WorkflowCallInput
	WorkflowCallSecret             = workflowlang.WorkflowCallSecret
	WorkflowCall                   = workflowlang.WorkflowCall
	Snapshot                       = workflowlang.Snapshot
	Job                            = workflowlang.Job
	Workflow                       = workflowlang.Workflow
)

const (
	WorkflowDispatchEventInputTypeNone        = workflowlang.WorkflowDispatchEventInputTypeNone
	WorkflowDispatchEventInputTypeString      = workflowlang.WorkflowDispatchEventInputTypeString
	WorkflowDispatchEventInputTypeNumber      = workflowlang.WorkflowDispatchEventInputTypeNumber
	WorkflowDispatchEventInputTypeBoolean     = workflowlang.WorkflowDispatchEventInputTypeBoolean
	WorkflowDispatchEventInputTypeChoice      = workflowlang.WorkflowDispatchEventInputTypeChoice
	WorkflowDispatchEventInputTypeEnvironment = workflowlang.WorkflowDispatchEventInputTypeEnvironment
	WorkflowCallEventInputTypeInvalid         = workflowlang.WorkflowCallEventInputTypeInvalid
	WorkflowCallEventInputTypeBoolean         = workflowlang.WorkflowCallEventInputTypeBoolean
	WorkflowCallEventInputTypeNumber          = workflowlang.WorkflowCallEventInputTypeNumber
	WorkflowCallEventInputTypeString          = workflowlang.WorkflowCallEventInputTypeString
	ExecKindAction                            = workflowlang.ExecKindAction
	ExecKindRun                               = workflowlang.ExecKindRun
	ExecKindWait                              = workflowlang.ExecKindWait
	ExecKindCancel                            = workflowlang.ExecKindCancel
	ExecKindParallel                          = workflowlang.ExecKindParallel
	RawYAMLValueKindObject                    = workflowlang.RawYAMLValueKindObject
	RawYAMLValueKindArray                     = workflowlang.RawYAMLValueKindArray
	RawYAMLValueKindString                    = workflowlang.RawYAMLValueKindString
)

// Parse returns a recovered syntax tree and all parser findings. A non-nil tree
// may accompany findings; parsing alone does not run workflow rules.
func Parse(source []byte) (*Workflow, []diagnostic.Diagnostic) {
	return workflowlang.ParseDocument(source)
}
