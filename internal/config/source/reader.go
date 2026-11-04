// Package source acquires local and public HTTPS configuration
// sources. Parsing and inheritance remain in package tree.
package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"

	"actionlint.kjanat.dev/v2/internal/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/config/address"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/network"
)

// MaxBytes bounds each remote configuration document independently of its format.
const MaxBytes = 4 << 20

// Reader binds acquisition to its host's context. A supplied reader owns all
// sources, including HTTPS addresses, and is never bypassed by network fallback.
type Reader struct {
	ctx      context.Context
	contents fsread.Contents
	client   *http.Client
}

func New(ctx context.Context, contents fsread.Contents) *Reader {
	return &Reader{ctx: ctx, contents: contents, client: &http.Client{
		Transport: publicHTTPS{},
		Timeout:   30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// A byte-only reader cannot report the final origin to inheritance.
			return errors.New("config redirects are unsupported; use the final HTTPS URL so relative extends keep their declaring origin")
		},
	}}
}

type publicHTTPS struct{}

func (publicHTTPS) RoundTrip(request *http.Request) (*http.Response, error) {
	transport, err := acquisitionhttp.Transport(request.URL, acquisitionhttp.Grant{})
	if err != nil {
		return nil, err
	}
	return transport.RoundTrip(request)
}

// ReadFile returns detached bytes. Remote acquisition is available only for an
// explicit HTTPS source address; ordinary filesystem reads cannot trigger it.
func (r *Reader) ReadFile(name string) ([]byte, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := address.Normalize(name); err != nil {
		return nil, err
	}
	var content []byte
	var err error
	switch {
	case r.contents != nil:
		content, err = r.contents(name)
	case !address.Remote(name):
		content, err = fsread.New(r.ctx, nil, nil).ReadFile(name)
	default:
		content, err = r.readHTTPS(r.ctx, name)
	}
	if err != nil {
		return nil, detachedReadError(err)
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if address.Remote(name) && len(content) > MaxBytes {
		return nil, fmt.Errorf("HTTPS config %q exceeds %d bytes", name, MaxBytes)
	}
	return bytes.Clone(content), nil
}

// Acquisition errors contain immutable failure data, never a transport's
// retained request or a custom reader's writable error. Filesystem and context
// classifications remain available through errors.Is and errors.As.
type readError struct {
	message                                                          string
	canceled, deadline, notExist, permission, invalid, exist, closed bool
}

func (e readError) Error() string { return e.message }
func (e readError) Is(target error) bool {
	return e.canceled && target == context.Canceled || e.deadline && target == context.DeadlineExceeded ||
		e.notExist && target == fs.ErrNotExist || e.permission && target == fs.ErrPermission ||
		e.invalid && target == fs.ErrInvalid || e.exist && target == fs.ErrExist || e.closed && target == fs.ErrClosed
}

func detachedReadError(err error) error {
	if pathError, ok := errors.AsType[*fs.PathError](err); ok {
		return &fs.PathError{Op: pathError.Op, Path: pathError.Path, Err: readErrorOf(pathError.Err)}
	}
	return readErrorOf(err)
}

func readErrorOf(err error) readError {
	return readError{
		message: err.Error(), canceled: errors.Is(err, context.Canceled), deadline: errors.Is(err, context.DeadlineExceeded),
		notExist: errors.Is(err, fs.ErrNotExist), permission: errors.Is(err, fs.ErrPermission), invalid: errors.Is(err, fs.ErrInvalid),
		exist: errors.Is(err, fs.ErrExist), closed: errors.Is(err, fs.ErrClosed),
	}
}

func (r *Reader) readHTTPS(ctx context.Context, name string) ([]byte, error) {
	ctx = network.WithOperation(ctx, network.Operation{Purpose: "remote-config", Priority: network.Required})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, name, nil)
	if err != nil {
		return nil, err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTPS config %q returned HTTP %d", name, response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > MaxBytes {
		return nil, fmt.Errorf("HTTPS config %q exceeds %d bytes", name, MaxBytes)
	}
	return content, nil
}
