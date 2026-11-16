package capture

import (
	"bytes"
	"context"
	"errors"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

// CaptureRemote records downloaded bytes without reading the URL or filesystem.
// Supplying different bytes for the same address invalidates its earlier captures.
func (s *Store) CaptureRemote(ctx context.Context, address string, content []byte, model sourcetext.Coordinates) (CapturedDocument, error) {
	if !sourcetext.RemoteAddress(address) || !model.Valid() {
		return CapturedDocument{}, errors.New("remote source capture requires an HTTPS address and coordinates")
	}
	document, err := s.Canonical(ctx, address)
	if err != nil {
		return CapturedDocument{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return CapturedDocument{}, &ClosedError{}
	}
	if err := ctx.Err(); err != nil {
		return CapturedDocument{}, err
	}
	if err := s.ctx.Err(); err != nil {
		return CapturedDocument{}, err
	}
	e := s.docs[document]
	if e.done == nil {
		e.model, e.done = model, make(chan struct{})
		e.value, err = capture(document, model, content)
		if err != nil {
			e.err = &CaptureError{document, err}
		}
		close(e.done)
	} else if e.value != nil && (e.model != model || !bytes.Equal(e.value.content, content)) {
		e.conflict = ConflictError{document, "remote source bytes or coordinates changed during analysis"}
	}
	if e.conflicted() {
		return CapturedDocument{}, e.conflict
	}
	if e.err != nil {
		return CapturedDocument{}, e.err
	}
	if e.readAliases == nil {
		e.readAliases = make(map[string]struct{})
	}
	e.readAliases[address] = struct{}{}
	return s.snapshot(e), nil
}
