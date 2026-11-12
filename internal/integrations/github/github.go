// Package github downloads action and reusable-workflow metadata using the
// configured network mode, credentials and cache directory.
package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

type Mode string

const (
	Off     Mode = "off"
	Offline Mode = "offline"
	Online  Mode = "online"
)

// Options selects acquisition explicitly. Tokens are never cached or included
// in returned failures. A supplied resolver must retain action selection routes.
type Options struct {
	Mode      Mode
	CacheDir  string
	Token     string
	Resolver  func() references.Resolver
	Transport http.RoundTripper
}

type GitHub struct {
	mode            Mode
	cacheDir, token string
	resolver        func() references.Resolver
	client          *http.Client
}

const (
	maxBody  = 1 << 20
	maxCache = 8 << 20
)

func New(options Options) (*GitHub, error) {
	if options.Mode == "" {
		options.Mode = Online
	}
	if options.Mode != Off && options.Mode != Offline && options.Mode != Online {
		return nil, errors.New("action metadata mode must be off, offline or online")
	}
	if strings.ContainsAny(options.Token, "\r\n\x00") {
		return nil, errors.New("invalid GitHub credential header")
	}
	return &GitHub{mode: options.Mode, cacheDir: options.CacheDir, token: options.Token, resolver: options.Resolver, client: &http.Client{
		Transport: accounting.TrackHTTPRequests(options.Transport), Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func fullSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func fullSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Lookup natively acquires only full SHA-1 commit requests. Named refs require
// an injected action-profile resolver; the commits API never selects their ref.
func (g *GitHub) Lookup(ctx context.Context, ref actions.Reference) (actions.Manifest, error) {
	file, err := g.lookupFile(ctx, ref, "action", []string{"action.yml", "action.yaml"})
	if err != nil {
		return actions.Manifest{}, err
	}
	if !file.Available() {
		return actions.UnavailableManifest(file.missing), nil
	}
	return actions.NewManifestFile(file.selection, file.filename, []byte(file.content))
}

func (g *GitHub) lookupFile(ctx context.Context, ref actions.Reference, kind string, filenames []string) (repositoryFile, error) {
	if err := ctx.Err(); err != nil {
		return repositoryFile{}, err
	}
	if g.mode == Off {
		return unavailableFile(availability.LookupDisabled()), nil
	}
	if ref.Host() != "github.com" {
		return unavailableFile(availability.UnsupportedHost()), nil
	}
	if g.mode == Offline {
		if !fullSHA1(ref.Ref()) {
			return unavailableFile(availability.Offline()), nil
		}
		return g.cachedFile(ctx, ref, filenames)
	}
	if fullSHA256(ref.Ref()) {
		return unavailableFile(availability.UnsupportedObjectFormat()), nil
	}
	if !fullSHA1(ref.Ref()) && g.resolver == nil {
		return unavailableFile(availability.ResolverNotConfigured()), nil
	}
	if fullSHA1(ref.Ref()) {
		cached, err := g.cachedFile(ctx, ref, filenames)
		if err != nil || cached.Available() {
			return cached, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ctx = accounting.WithRequestOptions(ctx, accounting.RequestOptions{Purpose: kind + "-metadata", Priority: accounting.Enrichment, RequestKind: accounting.REST, QuotaPool: "core"})
	if session := accounting.RequestTrackerFromContext(ctx); session != nil {
		request, err := g.request(ctx, repositoryURL(ref), false)
		if err != nil {
			return repositoryFile{}, err
		}
		if err := session.LoadGitHubRateLimits(ctx, g.client, request); err != nil {
			return repositoryFile{}, err
		}
		if err := session.CheckBatchQuota(request, 1); err != nil {
			return unavailableFile(availability.Limit()), nil
		}
	}
	repo, missing, err := g.repository(ctx, ref)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	commit := strings.ToLower(ref.Ref())
	object := commit
	route := actions.ObjectRoute()
	observed := time.Now().UTC().Format(time.RFC3339Nano)
	if g.resolver != nil {
		resolved, resolveErr := g.resolver().Resolve(ctx, references.Request{Version: 1, Repository: ref.Repository(), Ref: ref.Ref(), Kind: kind, ExpectedRepositoryID: repo.ID})
		if resolveErr != nil {
			return unavailableError(resolveErr)
		}
		if resolved.Version != 1 || resolved.Error != "" || resolved.RepositoryID != repo.ID || !strings.EqualFold(resolved.Repository, ref.Repository()) || !fullSHA1(resolved.Commit) || resolved.Tree != "" || resolved.ObservedAt == "" || len(resolved.FetchLog) == 0 {
			return unavailableFile(availability.InvalidResolverResult()), nil
		}
		if kind == "action" {
			if !fullSHA1(resolved.SelectedObject) {
				return unavailableFile(availability.InvalidResolverResult()), nil
			}
			if resolved.SelectedRef == "" && strings.EqualFold(ref.Ref(), resolved.SelectedObject) {
				route = actions.ObjectRoute()
			} else {
				route, err = actions.RefRoute(resolved.SelectedRef)
				if err != nil {
					return unavailableFile(availability.InvalidResolverResult()), nil
				}
			}
		}
		commit, object, observed = resolved.Commit, resolved.SelectedObject, resolved.ObservedAt
	}
	identity, err := actions.NewIdentity(ref.Host(), repo.Name, repo.ID, commit, ref.Directory())
	if err != nil {
		return unavailableFile(availability.Failed()), nil
	}
	var selection actions.Selection
	if kind == "action" {
		selection, err = actions.NewSelection(ref, identity, object, route, observed)
		if err != nil {
			return unavailableFile(availability.Failed()), nil
		}
	}
	file := repositoryFile{reference: ref, identity: identity, selection: selection, observedAt: observed}
	if cached, exists, cacheErr := g.cached(ctx, ref, repo.ID, commit, filenames); cacheErr == nil && exists {
		file.filename, file.content = cached.Filename, cached.Content
		return file, nil
	}
	if session := accounting.RequestTrackerFromContext(ctx); session != nil {
		request, err := g.request(ctx, repositoryURL(ref), false)
		if err != nil {
			return repositoryFile{}, err
		}
		if err := session.CheckBatchQuota(request, uint64(2+len(filenames))); err != nil {
			return unavailableFile(availability.Limit()), nil
		}
	}
	var objectResponse struct {
		SHA string `json:"sha"`
	}
	objectBytes, missing, err := g.get(ctx, repositoryURL(ref)+"/git/commits/"+commit, false)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	if json.Unmarshal(objectBytes, &objectResponse) != nil || !strings.EqualFold(objectResponse.SHA, commit) {
		return unavailableFile(availability.Failed()), nil
	}
	filename := filenames[0]
	content, missing, err := g.get(ctx, manifestURL(ref, commit, filename), true)
	if err != nil {
		return repositoryFile{}, err
	}
	if missing != nil && missing.Reason() == availability.InaccessibleReason && len(filenames) > 1 {
		filename = filenames[1]
		content, missing, err = g.get(ctx, manifestURL(ref, commit, filename), true)
	}
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	// Recheck the repository guard after name-addressed acquisition. This is an
	// observation, not a proof of uninterrupted ownership or runner execution.
	after, missing, err := g.repository(ctx, ref)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	if after.ID != repo.ID || !strings.EqualFold(after.Name, repo.Name) {
		return unavailableFile(availability.Failed()), nil
	}
	file.filename, file.content = filename, string(content)
	if err := g.store(ctx, file); err != nil {
		if ctx.Err() != nil {
			return repositoryFile{}, ctx.Err()
		}
		// The network observation is usable even when the user's cache cannot be written.
	}
	return file, nil
}

type repositoryIdentity struct {
	ID   uint64 `json:"id"`
	Name string `json:"full_name"`
}

func (g *GitHub) repository(ctx context.Context, ref actions.Reference) (repositoryIdentity, *availability.Unavailability, error) {
	var repo repositoryIdentity
	content, missing, err := g.get(ctx, repositoryURL(ref), false)
	if err == nil && missing == nil && (json.Unmarshal(content, &repo) != nil || repo.ID == 0 || !strings.EqualFold(repo.Name, ref.Repository())) {
		reason := availability.Failed()
		missing = &reason
	}
	return repo, missing, err
}

func repositoryURL(ref actions.Reference) string {
	parts := strings.Split(ref.Repository(), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "https://api.github.com/repos/" + strings.Join(parts, "/")
}

func manifestURL(ref actions.Reference, commit, filename string) string {
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
	return repositoryURL(ref) + "/contents/" + strings.Join(parts, "/") + "?ref=" + url.QueryEscape(commit)
}

func (g *GitHub) request(ctx context.Context, address string, raw bool) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("invalid GitHub metadata request")
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if raw {
		request.Header.Set("Accept", "application/vnd.github.raw+json")
	}
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", "actionlint")
	if g.token != "" {
		request.Header.Set("Authorization", "Bearer "+g.token)
	}
	return request, nil
}

func (g *GitHub) get(ctx context.Context, address string, raw bool) ([]byte, *availability.Unavailability, error) {
	request, err := g.request(ctx, address, raw)
	if err != nil {
		return nil, nil, err
	}
	response, err := g.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		reason := availability.Failed()
		if errors.Is(err, accounting.ErrRateLimited) || errors.Is(err, accounting.ErrBudget) {
			reason = availability.Limit()
		}
		return nil, &reason, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		reason := availability.Failed()
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			reason = availability.Denied()
			if response.StatusCode == http.StatusForbidden && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "") {
				reason = availability.Limit()
			}
		case http.StatusNotFound:
			reason = availability.Inaccessible()
		case http.StatusTooManyRequests:
			reason = availability.Limit()
		}
		return nil, &reason, nil
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		reason := availability.Failed()
		return nil, &reason, nil
	}
	if len(content) > maxBody {
		reason := availability.Limit()
		return nil, &reason, nil
	}
	return content, nil, nil
}

func unavailable(missing *availability.Unavailability, err error) (repositoryFile, error) {
	if err != nil {
		return repositoryFile{}, err
	}
	if missing == nil {
		return unavailableFile(availability.Failed()), nil
	}
	return unavailableFile(*missing), nil
}

func unavailableError(err error) (repositoryFile, error) {
	if errors.Is(err, context.Canceled) {
		return repositoryFile{}, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return repositoryFile{}, context.DeadlineExceeded
	}
	return unavailableFile(availability.Failed()), nil
}

type cacheRecord struct {
	Version      int    `json:"version"`
	Host         string `json:"host"`
	Repository   string `json:"repository"`
	RepositoryID uint64 `json:"repository_id"`
	Commit       string `json:"commit"`
	Directory    string `json:"directory"`
	ObservedAt   string `json:"observed_at"`
	Filename     string `json:"filename"`
	Content      string `json:"content"`
	SHA256       string `json:"sha256"`
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (g *GitHub) bucket(ref actions.Reference, commit string) string {
	return filepath.Join(g.cacheDir, digest(ref.Host()+"\x00"+strings.ToLower(ref.Repository())+"\x00"+strings.ToLower(commit)+"\x00"+ref.Directory()))
}

func (g *GitHub) cached(ctx context.Context, ref actions.Reference, id uint64, commit string, filenames []string) (cacheRecord, bool, error) {
	if g.cacheDir == "" {
		return cacheRecord{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return cacheRecord{}, false, err
	}
	reader := files.New(ctx, nil, nil)
	file, err := reader.Open(filepath.Join(g.bucket(ref, commit), strconv.FormatUint(id, 10)+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return cacheRecord{}, false, nil
	}
	if err != nil {
		return cacheRecord{}, false, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxCache+1))
	if err != nil {
		return cacheRecord{}, false, err
	}
	var record cacheRecord
	if len(content) > maxCache || json.Unmarshal(content, &record) != nil || record.Version != 1 || record.Host != ref.Host() || !strings.EqualFold(record.Repository, ref.Repository()) || record.RepositoryID != id || !strings.EqualFold(record.Commit, commit) || record.Directory != ref.Directory() || record.SHA256 != digest(record.Content) || len(record.Content) > maxBody || !slices.Contains(filenames, record.Filename) {
		return cacheRecord{}, false, errors.New("invalid repository metadata cache record")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.ObservedAt); err != nil {
		return cacheRecord{}, false, errors.New("invalid repository metadata cache observation")
	}
	if err := ctx.Err(); err != nil {
		return cacheRecord{}, false, err
	}
	return record, true, nil
}

func (g *GitHub) cachedFile(ctx context.Context, ref actions.Reference, filenames []string) (repositoryFile, error) {
	if g.cacheDir == "" {
		return unavailableFile(availability.CacheMiss()), nil
	}
	reader := files.New(ctx, nil, nil)
	bucket := g.bucket(ref, ref.Ref())
	directory, err := reader.Open(bucket)
	if errors.Is(err, os.ErrNotExist) {
		return unavailableFile(availability.CacheMiss()), nil
	}
	if err != nil {
		return unavailableFile(availability.Failed()), nil
	}
	defer directory.Close()
	// A bucket contains one repository-ID record. Bound enumeration too;
	// ambiguous identities or excessive debris must not choose an arbitrary hit.
	entries, err := directory.ReadDir(3)
	if err != nil && !errors.Is(err, io.EOF) {
		return unavailableFile(availability.Failed()), nil
	}
	if err := ctx.Err(); err != nil {
		return repositoryFile{}, err
	}
	if len(entries) == 3 {
		return unavailableFile(availability.Failed()), nil
	}
	var id uint64
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() {
			continue
		}
		parsed, err := strconv.ParseUint(name, 10, 64)
		if err != nil || parsed == 0 {
			continue
		}
		if id != 0 {
			return unavailableFile(availability.Failed()), nil
		}
		id = parsed
	}
	if id == 0 {
		return unavailableFile(availability.CacheMiss()), nil
	}
	record, exists, err := g.cached(ctx, ref, id, ref.Ref(), filenames)
	if err != nil {
		if ctx.Err() != nil {
			return repositoryFile{}, ctx.Err()
		}
		return unavailableFile(availability.Failed()), nil
	}
	if !exists {
		return unavailableFile(availability.CacheMiss()), nil
	}
	identity, err := actions.NewIdentity(record.Host, record.Repository, id, record.Commit, record.Directory)
	if err != nil {
		return unavailableFile(availability.Failed()), nil
	}
	var selection actions.Selection
	if record.Filename != "" {
		selection, err = actions.NewSelection(ref, identity, strings.ToLower(ref.Ref()), actions.ObjectRoute(), record.ObservedAt)
		if err != nil {
			return unavailableFile(availability.Failed()), nil
		}
	}
	return repositoryFile{reference: ref, identity: identity, selection: selection, observedAt: record.ObservedAt, filename: record.Filename, content: record.Content}, nil
}

func (g *GitHub) store(ctx context.Context, manifest repositoryFile) error {
	if g.cacheDir == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	i := manifest.Identity()
	record := cacheRecord{Version: 1, Host: i.Host(), Repository: i.Repository(), RepositoryID: i.RepositoryID(), Commit: i.Commit(), Directory: i.Directory(), ObservedAt: manifest.observedAt, Filename: manifest.Filename(), Content: manifest.Content(), SHA256: digest(manifest.Content())}
	content, err := json.Marshal(record)
	if err != nil {
		return err
	}
	bucket := g.bucket(manifest.reference, i.Commit())
	if err := os.MkdirAll(bucket, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(bucket, ".metadata-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(bucket, strconv.FormatUint(i.RepositoryID(), 10)+".json"))
}
