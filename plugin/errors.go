package plugin

import (
	"context"
	"errors"
	"io"
	"io/fs"

	"actionlint.kjanat.dev/v2/analyzer"
)

// Runtime failures are immutable snapshots: exact text, standard context/fs/io
// classifications, and detached public fallback/path details. Guest/runtime
// implementation error types are not part of this facade's errors.As contract.
// Acquisition and caller-supplied OCI hook errors pass through unchanged.
type runtimeFailure struct {
	message                       string
	classes                       failureClasses
	hasPath                       bool
	pathOp, pathName, pathMessage string
	pathClasses                   failureClasses
	hasUnsupported                bool
	unsupportedReason             string
}

type failureClasses struct {
	canceled, deadline, unsupported                                     bool
	invalid, permission, exist, notExist, closed, skipDir, skipAll      bool
	eof, unexpectedEOF, closedPipe, noProgress, shortBuffer, shortWrite bool
}

func (e runtimeFailure) Error() string { return e.message }

func (e runtimeFailure) Is(target error) bool { return e.classes.matches(target) }

// Unwrap constructs fresh details on each call. Callers may inspect and mutate
// those ordinary public error values without changing later observations.
func (e runtimeFailure) Unwrap() []error {
	var details []error
	if e.hasPath {
		details = append(details, &fs.PathError{Op: e.pathOp, Path: e.pathName, Err: runtimeFailure{message: e.pathMessage, classes: e.pathClasses}})
	}
	if e.hasUnsupported {
		details = append(details, &analyzer.UnsupportedToolConfigurationError{Reason: e.unsupportedReason})
	}
	return details
}

func snapshotRuntimeFailure(err error) error {
	if err == nil {
		return nil
	}
	failure := runtimeFailure{message: err.Error(), classes: classifyFailure(err)}
	if path, ok := errors.AsType[*fs.PathError](err); ok {
		failure.hasPath = true
		failure.pathOp, failure.pathName, failure.pathMessage = path.Op, path.Path, path.Err.Error()
		failure.pathClasses = classifyFailure(path.Err)
	}
	if unsupported, ok := errors.AsType[*analyzer.UnsupportedToolConfigurationError](err); ok {
		failure.hasUnsupported = true
		failure.unsupportedReason = unsupported.Reason
	}
	return failure
}

func classifyFailure(err error) failureClasses {
	return failureClasses{
		canceled: errors.Is(err, context.Canceled), deadline: errors.Is(err, context.DeadlineExceeded), unsupported: errors.Is(err, errors.ErrUnsupported),
		invalid: errors.Is(err, fs.ErrInvalid), permission: errors.Is(err, fs.ErrPermission), exist: errors.Is(err, fs.ErrExist), notExist: errors.Is(err, fs.ErrNotExist), closed: errors.Is(err, fs.ErrClosed), skipDir: errors.Is(err, fs.SkipDir), skipAll: errors.Is(err, fs.SkipAll),
		eof: errors.Is(err, io.EOF), unexpectedEOF: errors.Is(err, io.ErrUnexpectedEOF), closedPipe: errors.Is(err, io.ErrClosedPipe), noProgress: errors.Is(err, io.ErrNoProgress), shortBuffer: errors.Is(err, io.ErrShortBuffer), shortWrite: errors.Is(err, io.ErrShortWrite),
	}
}

func (c failureClasses) matches(target error) bool {
	return c.canceled && target == context.Canceled || c.deadline && target == context.DeadlineExceeded || c.unsupported && target == errors.ErrUnsupported ||
		c.invalid && target == fs.ErrInvalid || c.permission && target == fs.ErrPermission || c.exist && target == fs.ErrExist || c.notExist && target == fs.ErrNotExist || c.closed && target == fs.ErrClosed || c.skipDir && target == fs.SkipDir || c.skipAll && target == fs.SkipAll ||
		c.eof && target == io.EOF || c.unexpectedEOF && target == io.ErrUnexpectedEOF || c.closedPipe && target == io.ErrClosedPipe || c.noProgress && target == io.ErrNoProgress || c.shortBuffer && target == io.ErrShortBuffer || c.shortWrite && target == io.ErrShortWrite
}
