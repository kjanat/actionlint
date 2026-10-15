// Package syntax exposes workflow syntax trees and parser diagnostics.
package syntax

import (
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/engine"
)

type Pos = engine.Pos
type String = engine.String
type Bool = engine.Bool
type Int = engine.Int
type Float = engine.Float
type Event = engine.Event
type WebhookEventFilter = engine.WebhookEventFilter
type WebhookEvent = engine.WebhookEvent
type ScheduleEntry = engine.ScheduleEntry
type ScheduledEvent = engine.ScheduledEvent
type WorkflowDispatchEventInputType = engine.WorkflowDispatchEventInputType
type DispatchInput = engine.DispatchInput
type WorkflowDispatchEvent = engine.WorkflowDispatchEvent
type RepositoryDispatchEvent = engine.RepositoryDispatchEvent
type WorkflowCallEventInputType = engine.WorkflowCallEventInputType
type WorkflowCallEventInput = engine.WorkflowCallEventInput
type WorkflowCallEventSecret = engine.WorkflowCallEventSecret
type WorkflowCallEventOutput = engine.WorkflowCallEventOutput
type WorkflowCallEvent = engine.WorkflowCallEvent
type ImageVersionEvent = engine.ImageVersionEvent
type PermissionScope = engine.PermissionScope
type Permissions = engine.Permissions
type DefaultsRun = engine.DefaultsRun
type Defaults = engine.Defaults
type Concurrency = engine.Concurrency
type Environment = engine.Environment
type ExecKind = engine.ExecKind
type Exec = engine.Exec
type ExecRun = engine.ExecRun
type Input = engine.Input
type ActionInputMapping = engine.ActionInputMapping
type ExecAction = engine.ExecAction
type ExecWait = engine.ExecWait
type ExecCancel = engine.ExecCancel
type ExecParallel = engine.ExecParallel
type RawYAMLValueKind = engine.RawYAMLValueKind
type RawYAMLValue = engine.RawYAMLValue
type RawYAMLObject = engine.RawYAMLObject
type RawYAMLArray = engine.RawYAMLArray
type RawYAMLString = engine.RawYAMLString
type MatrixRow = engine.MatrixRow
type MatrixAssign = engine.MatrixAssign
type MatrixCombination = engine.MatrixCombination
type MatrixCombinations = engine.MatrixCombinations
type Matrix = engine.Matrix
type Strategy = engine.Strategy
type EnvVar = engine.EnvVar
type Env = engine.Env
type Step = engine.Step
type Credentials = engine.Credentials
type Container = engine.Container
type Service = engine.Service
type Services = engine.Services
type Output = engine.Output
type Runner = engine.Runner
type WorkflowCallInput = engine.WorkflowCallInput
type WorkflowCallSecret = engine.WorkflowCallSecret
type WorkflowCall = engine.WorkflowCall
type Snapshot = engine.Snapshot
type Job = engine.Job
type Workflow = engine.Workflow

const (
	WorkflowDispatchEventInputTypeNone        = engine.WorkflowDispatchEventInputTypeNone
	WorkflowDispatchEventInputTypeString      = engine.WorkflowDispatchEventInputTypeString
	WorkflowDispatchEventInputTypeNumber      = engine.WorkflowDispatchEventInputTypeNumber
	WorkflowDispatchEventInputTypeBoolean     = engine.WorkflowDispatchEventInputTypeBoolean
	WorkflowDispatchEventInputTypeChoice      = engine.WorkflowDispatchEventInputTypeChoice
	WorkflowDispatchEventInputTypeEnvironment = engine.WorkflowDispatchEventInputTypeEnvironment
	WorkflowCallEventInputTypeInvalid         = engine.WorkflowCallEventInputTypeInvalid
	WorkflowCallEventInputTypeBoolean         = engine.WorkflowCallEventInputTypeBoolean
	WorkflowCallEventInputTypeNumber          = engine.WorkflowCallEventInputTypeNumber
	WorkflowCallEventInputTypeString          = engine.WorkflowCallEventInputTypeString
	ExecKindAction                            = engine.ExecKindAction
	ExecKindRun                               = engine.ExecKindRun
	ExecKindWait                              = engine.ExecKindWait
	ExecKindCancel                            = engine.ExecKindCancel
	ExecKindParallel                          = engine.ExecKindParallel
	RawYAMLValueKindObject                    = engine.RawYAMLValueKindObject
	RawYAMLValueKindArray                     = engine.RawYAMLValueKindArray
	RawYAMLValueKindString                    = engine.RawYAMLValueKindString
)

// Parse returns a recovered syntax tree and all parser findings. A non-nil tree
// may accompany findings; parsing alone does not run workflow rules.
func Parse(source []byte) (*Workflow, []diagnostic.Diagnostic) { return engine.ParseDocument(source) }
