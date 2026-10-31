package api

import "bytes"

// Text only searches Content with bytes.IndexByte and bytes.HasPrefix, so its
// fields stay a plain value record.
type Text struct {
	Content []byte
	Prefix  []byte
}

func (t *Text) Lines() int {
	lines := 0
	for rest := t.Content; ; lines++ {
		end := bytes.IndexByte(rest, '\n')
		if end < 0 {
			return lines + 1
		}
		rest = rest[end+1:]
	}
}

func (t *Text) Prefixed() bool { return bytes.HasPrefix(t.Content, t.Prefix) }

// bytes.Cut returns subslices of its argument and has no model.
type Header struct {
	Raw []byte // want "ARC004: type Header exposes writable field Raw"
}

func (h *Header) Name() []byte { // want "ARC004: Name result 1"
	name, _, _ := bytes.Cut(h.Raw, []byte(":"))
	return name
}
