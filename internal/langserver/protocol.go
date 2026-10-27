// Package langserver adapts versioned editor buffers to analysis and source fixes.
package langserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/fix"
)

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type sourceRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func readMessage(r *bufio.Reader) (message, error) {
	length := -1
	headers := 0
	for {
		raw, err := r.ReadSlice('\n')
		line := string(raw)
		if err != nil {
			if errors.Is(err, bufio.ErrBufferFull) {
				return message{}, errors.New("LSP header line exceeds limit")
			}
			return message{}, err
		}
		headers += len(line)
		if headers > 8192 {
			return message{}, errors.New("LSP headers exceed limit")
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			if length != -1 {
				return message{}, errors.New("duplicate Content-Length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return message{}, err
			}
		}
	}
	if length < 0 || length > 16<<20 {
		return message{}, fmt.Errorf("invalid LSP message length %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return message{}, err
	}
	var m message
	err := jsonv2.Unmarshal(data, &m)
	return m, err
}

func writeMessage(w io.Writer, value any) error {
	data, err := jsonv2.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func filePath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
		return "", errors.New("expected local file URI")
	}
	path := u.Path
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		if runtime.GOOS != "windows" || u.Host != u.Hostname() {
			return "", errors.New("expected local file URI")
		}
		path = "//" + u.Host + path
	} else if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		return "", errors.New("expected absolute file URI")
	}
	return filepath.Clean(path), nil
}

// FileURI converts an absolute local path to an editor URI, including drive paths.
func FileURI(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" && strings.HasPrefix(path, "//") {
		host, rest, _ := strings.Cut(path[2:], "/")
		return (&url.URL{Scheme: "file", Host: host, Path: "/" + rest}).String()
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func editorPosition(content []byte, p diagnostic.Position) (position, error) {
	offset, err := fix.Offset(content, p)
	if err != nil {
		return position{}, err
	}
	return positionAtByteOffset(content, offset), nil
}

func positionAtByteOffset(content []byte, offset int) position {
	line, units := 0, 0
	start := 0
	if bytes.HasPrefix(content, []byte("\ufeff")) {
		start = len("\ufeff")
	}
	for i := start; i < offset; {
		r, n := utf8.DecodeRune(content[i:])
		if r == '\r' && i+n < len(content) && content[i+n] == '\n' {
			n++
		}
		if r == '\r' || r == '\n' {
			line++
			units = 0
		} else {
			units += utf16.RuneLen(r)
		}
		i += n
	}
	return position{line, units}
}

func byteOffset(text string, p position) (int, error) {
	if p.Line < 0 || p.Character < 0 {
		return 0, errors.New("negative editor position")
	}
	line, units := 0, 0
	start := 0
	if strings.HasPrefix(text, "\ufeff") {
		start = len("\ufeff")
	}
	for offset := start; ; {
		if line == p.Line && units == p.Character {
			return offset, nil
		}
		if offset == len(text) {
			break
		}
		r, n := utf8.DecodeRuneInString(text[offset:])
		if r == '\r' && offset+n < len(text) && text[offset+n] == '\n' {
			n++
		}
		if r == '\n' || r == '\r' {
			line++
			units = 0
		} else {
			units += utf16.RuneLen(r)
		}
		offset += n
		if line > p.Line || line == p.Line && units > p.Character {
			return 0, errors.New("invalid UTF-16 position")
		}
	}
	return 0, errors.New("editor position outside buffer")
}

func comparePosition(a, b position) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Character - b.Character
}

func diagnosticRange(content []byte, start, end diagnostic.Position) (sourceRange, error) {
	first, err := editorPosition(content, start)
	if err != nil {
		return sourceRange{}, err
	}
	if end == (diagnostic.Position{}) {
		return sourceRange{first, first}, nil
	}
	last, err := editorPosition(content, end)
	if err != nil {
		return sourceRange{}, err
	}
	if comparePosition(last, first) < 0 {
		last = first
	}
	return sourceRange{first, last}, nil
}
