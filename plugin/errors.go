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
type runtimeError struct {
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

func (e runtimeError) Error() string { return e.message }

func (e runtimeError) Is(target error) bool {
	return e.classes.canceled && target == context.Canceled || e.classes.deadline && target == context.DeadlineExceeded || e.classes.unsupported && target == errors.ErrUnsupported ||
		e.classes.invalid && target == fs.ErrInvalid || e.classes.permission && target == fs.ErrPermission || e.classes.exist && target == fs.ErrExist || e.classes.notExist && target == fs.ErrNotExist || e.classes.closed && target == fs.ErrClosed || e.classes.skipDir && target == fs.SkipDir || e.classes.skipAll && target == fs.SkipAll ||
		e.classes.eof && target == io.EOF || e.classes.unexpectedEOF && target == io.ErrUnexpectedEOF || e.classes.closedPipe && target == io.ErrClosedPipe || e.classes.noProgress && target == io.ErrNoProgress || e.classes.shortBuffer && target == io.ErrShortBuffer || e.classes.shortWrite && target == io.ErrShortWrite
}

// Unwrap constructs fresh details on each call. Callers may inspect and mutate
// those ordinary public error values without changing later observations.
func (e runtimeError) Unwrap() []error {
	var details []error
	if e.hasPath {
		details = append(details, &fs.PathError{Op: e.pathOp, Path: e.pathName, Err: runtimeError{message: e.pathMessage, classes: e.pathClasses}})
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
	failure := runtimeError{message: err.Error(), classes: classifyFailure(err)}
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
