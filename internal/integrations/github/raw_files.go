package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

func escapedFilePath(ref actions.Reference, filename string) string {
	var parts []string
	if ref.Directory() != "" {
		parts = strings.Split(ref.Directory(), "/")
	}
	if filename != "" {
		parts = append(parts, filename)
	}
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func rawFileURL(ref actions.Reference, commit, filename string) string {
	owner, repo, _ := strings.Cut(ref.Repository(), "/")
	return "https://raw.githubusercontent.com/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/" + url.PathEscape(strings.ToLower(commit)) + "/" + escapedFilePath(ref, filename)
}

func jsDelivrFileURL(ref actions.Reference, commit, filename string) string {
	owner, repo, _ := strings.Cut(ref.Repository(), "/")
	return "https://cdn.jsdelivr.net/gh/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "@" + url.PathEscape(strings.ToLower(commit)) + "/" + escapedFilePath(ref, filename)
}

type exchanged struct {
	status int
	header http.Header
	body   []byte
}

type rawNotFoundKey struct{}

func (g *GitHub) exchange(ctx context.Context, request *http.Request, limit int) (exchanged, error) {
	if err := references.WaitOutsideLookupBudget(ctx, func() error { return g.requests.Acquire(ctx, 1) }); err != nil {
		return exchanged{}, err
	}
	defer g.requests.Release(1)
	response, err := g.client.Do(request)
	if err != nil {
		return exchanged{}, err
	}
	defer response.Body.Close()
	result := exchanged{status: response.StatusCode, header: response.Header}
	if response.StatusCode != http.StatusOK {
		return result, nil
	}
	result.body, err = io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	return result, err
}

func (g *GitHub) fetchRawFile(ctx context.Context, ref actions.Reference, commit, kind string, filenames []string) (string, string, *availability.Unavailability, error) {
	if _, _, ok := splitRepository(ref.Repository()); !ok {
		reason := availability.Failed()
		return "", "", &reason, nil
	}
	ctx = accounting.WithRequestOptions(ctx, accounting.RequestOptions{Purpose: kind + "-metadata", Priority: accounting.Enrichment, RequestKind: accounting.HTTP})
	raw, err := g.fetchRawHostFile(ctx, ref, commit, filenames)
	priorNotFound, _ := ctx.Value(rawNotFoundKey{}).(bool)
	if !errors.Is(err, errEnvironment) || raw.notFound || priorNotFound || !fullSHA1(commit) || !g.mirrorAllowed(ref.Repository()) {
		return raw.filename, raw.content, raw.missing, err
	}
	filename, content, found, cdnErr := g.fetchJSDelivrFile(ctx, ref, commit, filenames)
	switch {
	case cdnErr != nil && !errors.Is(cdnErr, errEnvironment):
		return "", "", nil, cdnErr
	case cdnErr != nil:
		return "", "", nil, errors.Join(err, cdnErr)
	case !found:
		return "", "", nil, err
	}
	return filename, content, nil, nil
}

type rawFileLookup struct {
	filename, content string
	missing           *availability.Unavailability
	notFound          bool
}

func (g *GitHub) fetchRawHostFile(ctx context.Context, ref actions.Reference, commit string, filenames []string) (rawFileLookup, error) {
	var result rawFileLookup
	for _, filename := range filenames {
		address := rawFileURL(ref, commit, filename)
		authenticated := g.token != ""
		content, found, missing, err := g.rawRequest(ctx, address, authenticated)
		if err == nil && missing == nil && !found {
			result.notFound = true
			if g.token != "" {
				authenticated = false
				content, found, missing, err = g.rawRequest(ctx, address, false)
			}
		}
		if err != nil || missing != nil {
			result.missing = missing
			return result, err
		}
		if found {
			if !authenticated {
				g.listings.observeVisibility(ref.Repository(), new(false))
			}
			result.filename, result.content = filename, content
			return result, nil
		}
	}
	reason := availability.Inaccessible()
	result.missing = &reason
	return result, nil
}

func (g *GitHub) fetchJSDelivrFile(ctx context.Context, ref actions.Reference, commit string, filenames []string) (string, string, bool, error) {
	for _, filename := range filenames {
		content, found, err := g.jsDelivrRequest(ctx, jsDelivrFileURL(ref, commit, filename))
		if err != nil {
			return "", "", false, err
		}
		if found {
			return filename, content, true, nil
		}
	}
	return "", "", false, nil
}

func (g *GitHub) jsDelivrRequest(ctx context.Context, address string) (string, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", false, errors.New("invalid jsDelivr file request")
	}
	request.Header.Set("User-Agent", "actionlint")
	response, err := g.exchange(ctx, request, maxBody)
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", false, fmt.Errorf("%w: jsDelivr file request failed: %w", errEnvironment, err)
	}
	switch {
	case response.status == http.StatusNotFound:
		return "", false, nil
	case response.status != http.StatusOK:
		return "", false, fmt.Errorf("%w: jsDelivr status %d", errEnvironment, response.status)
	case mediaType(response.header) != "text/yaml":
		return "", false, fmt.Errorf("%w: unexpected jsDelivr file response", errEnvironment)
	case len(response.body) > maxBody:
		return "", false, fmt.Errorf("%w: oversized jsDelivr file response", errEnvironment)
	}
	return string(response.body), true, nil
}

func (g *GitHub) rawRequest(ctx context.Context, address string, authenticated bool) (string, bool, *availability.Unavailability, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", false, nil, errors.New("invalid GitHub raw file request")
	}
	request.Header.Set("User-Agent", "actionlint")
	if authenticated {
		request.Header.Set("Authorization", "Bearer "+g.token)
	}
	response, err := g.exchange(ctx, request, maxBody)
	if err != nil {
		if ctx.Err() != nil {
			return "", false, nil, ctx.Err()
		}
		return "", false, nil, fmt.Errorf("%w: raw file request failed: %w", errEnvironment, err)
	}
	switch {
	case response.status == http.StatusNotFound:
		return "", false, nil, nil
	case response.status != http.StatusOK:
		return "", false, unavailableStatus(response.status), environmentStatus(response.status)
	case mediaType(response.header) != "text/plain":
		return "", false, nil, fmt.Errorf("%w: unexpected raw file response", errEnvironment)
	case len(response.body) > maxBody:
		reason := availability.Limit()
		return "", false, &reason, nil
	}
	return string(response.body), true, nil, nil
}
