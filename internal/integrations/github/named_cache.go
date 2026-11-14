package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

type cachedRef struct {
	Name         string          `json:"name"`
	Commit       string          `json:"commit"`
	Object       string          `json:"object"`
	Ref          string          `json:"ref"`
	RepositoryID repositoryid.ID `json:"repository_id,omitzero"`
}

func (g *GitHub) namedCachePath(ref actions.Reference) string {
	return filepath.Join(g.cacheDir, "refs", digest(ref.Host()+"\x00"+strings.ToLower(ref.Repository())+"\x00"+ref.Ref()+"\x00"+ref.Directory())+".json")
}

func (g *GitHub) storeNamed(ctx context.Context, file repositoryFile) (err error) {
	if g.cacheDir == "" || fullSHA1(file.reference.Ref()) || fullSHA256(file.reference.Ref()) {
		return nil
	}
	path := g.namedCachePath(file.reference)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	output, err := os.CreateTemp(filepath.Dir(path), ".ref-*")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.Remove(output.Name()); !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	writeErr := json.NewEncoder(output).Encode(cachedRef{file.reference.Ref(), file.identity.Commit(), file.selection.Object(), file.selection.Route().Reference(), file.identity.RepositoryID()})
	if err := errors.Join(writeErr, output.Close(), ctx.Err()); err != nil {
		return err
	}
	return os.Rename(output.Name(), path)
}

func (g *GitHub) offlineNamed(ctx context.Context, ref actions.Reference, kind string, filenames []string) (repositoryFile, error) {
	if g.cacheDir == "" {
		return unavailableFile(availability.Offline()), nil
	}
	input, err := files.New(ctx, nil, nil).Open(g.namedCachePath(ref))
	if errors.Is(err, os.ErrNotExist) {
		return unavailableFile(availability.Offline()), nil
	}
	if err != nil {
		return repositoryFile{}, err
	}
	defer input.Close()
	data, err := io.ReadAll(io.LimitReader(input, 4097))
	var named cachedRef
	if err != nil || len(data) > 4096 || json.Unmarshal(data, &named) != nil || named.Name != ref.Ref() || !fullSHA1(named.Commit) {
		return repositoryFile{}, errors.New("invalid cached Git reference")
	}
	record, exists, err := g.cached(ctx, ref, named.RepositoryID, named.Commit, filenames)
	if err != nil {
		return repositoryFile{}, err
	}
	if !exists {
		return unavailableFile(availability.CacheMiss()), nil
	}
	route := actions.ObjectRoute()
	if named.Ref != "" {
		route, err = actions.RefRoute(named.Ref)
		if err != nil {
			return repositoryFile{}, err
		}
	}
	file, valid := selectedFile(ref, record.Repository, kind, record.RepositoryID, named.Commit, named.Object, route, record.ObservedAt)
	if !valid {
		return repositoryFile{}, errors.New("invalid cached Git reference selection")
	}
	file.filename, file.content = record.Filename, record.Content
	return file, ctx.Err()
}
