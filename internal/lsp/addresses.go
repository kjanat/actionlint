package lsp

import (
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

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
