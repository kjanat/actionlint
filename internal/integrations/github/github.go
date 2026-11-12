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
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
	"golang.org/x/sync/semaphore"
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
	Mirrors   references.MirrorAccess
}

type GitHub struct {
	mode            Mode
	cacheDir, token string
	resolver        func() references.Resolver
	client          *http.Client
	listings        *refListings
	requests        *semaphore.Weighted
	mirrors         references.MirrorAccess
}

const (
	maxBody             = 1 << 20
	maxCache            = 8 << 20
	concurrentTransfers = 16
	lookupBudget        = 30 * time.Second
)

func New(options Options) (*GitHub, error) {
	if options.Mirrors > references.NoMirrors {
		return nil, errors.New("invalid metadata mirror access")
	}
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
	}, listings: newRefListings(), requests: semaphore.NewWeighted(concurrentTransfers), mirrors: options.Mirrors}, nil
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
		return g.offline(ctx, ref, filenames)
	}
	if fullSHA256(ref.Ref()) {
		return unavailableFile(availability.UnsupportedObjectFormat()), nil
	}
	if !fullSHA1(ref.Ref()) && g.resolver == nil {
		return unavailableFile(availability.ResolverNotConfigured()), nil
	}
	budgeted, stop := references.StartLookupBudget(ctx, lookupBudget)
	defer stop()
	var (
		file repositoryFile
		err  error
	)
	if fullSHA1(ref.Ref()) {
		file, err = g.lookupCommitFile(budgeted, ref, kind, filenames)
	} else {
		file, err = g.lookupNamedFile(budgeted, ref, kind, filenames)
	}
	if errors.Is(err, errEnvironment) {
		file, err = g.lookupFileREST(budgeted, ref, kind, filenames)
	}
	if err != nil && ctx.Err() == nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return unavailableFile(availability.Failed()), nil
	}
	return file, err
}

func (g *GitHub) lookupCommitFile(ctx context.Context, ref actions.Reference, kind string, filenames []string) (repositoryFile, error) {
	if kind == "workflow" && ref.Ref() != strings.ToLower(ref.Ref()) {
		return unavailableFile(availability.Failed()), nil
	}
	commit := strings.ToLower(ref.Ref())
	record, exists, err := g.cached(ctx, ref, repositoryid.ID{}, commit, filenames)
	if err != nil && ctx.Err() != nil {
		return repositoryFile{}, ctx.Err()
	}
	if exists {
		if file, valid := cachedRepositoryFile(ref, record); valid {
			return file, nil
		}
	}
	file, err := g.fetchSelectedFile(ctx, ref, kind, filenames, repositoryid.ID{}, commit, commit, actions.ObjectRoute(), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil || file.Available() || file.missing.Reason() != availability.InaccessibleReason || g.resolver == nil {
		return file, err
	}
	return g.lookupTagObjectFile(ctx, ref, kind, filenames, commit, file)
}

// raw.githubusercontent.com serves files only at commit IDs, while GitHub runs
// actions pinned to an annotated tag object ID.
func (g *GitHub) lookupTagObjectFile(ctx context.Context, ref actions.Reference, kind string, filenames []string, object string, inaccessible repositoryFile) (repositoryFile, error) {
	listing, missing, err := g.listings.get(ctx, g, ref.Repository())
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	if _, tag := listing.peeled[object]; !tag {
		return inaccessible, nil
	}
	return g.resolveListedFile(context.WithValue(ctx, rawNotFoundKey{}, true), ref, kind, filenames, listing)
}

func (g *GitHub) lookupNamedFile(ctx context.Context, ref actions.Reference, kind string, filenames []string) (repositoryFile, error) {
	listing, missing, err := g.listings.get(ctx, g, ref.Repository())
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	return g.resolveListedFile(ctx, ref, kind, filenames, listing)
}

func (g *GitHub) resolveListedFile(ctx context.Context, ref actions.Reference, kind string, filenames []string, listing *refListing) (repositoryFile, error) {
	resolved, err := g.resolver().Resolve(withRefListing(ctx, listing), references.Request{Version: 1, Repository: ref.Repository(), Ref: ref.Ref(), Kind: kind})
	if err != nil {
		return unavailableError(err)
	}
	commit, object, route, missing := selectResolution(ref, kind, resolved, repositoryid.ID{})
	if missing != nil {
		return unavailableFile(*missing), nil
	}
	record, exists, err := g.cached(ctx, ref, resolved.RepositoryID, commit, filenames)
	if err != nil && ctx.Err() != nil {
		return repositoryFile{}, ctx.Err()
	}
	if exists {
		id := resolved.RepositoryID
		if id.IsZero() {
			id = record.RepositoryID
		}
		file, valid := selectedFile(ref, ref.Repository(), kind, id, commit, object, route, resolved.ObservedAt)
		if !valid {
			return unavailableFile(availability.Failed()), nil
		}
		file.filename, file.content = record.Filename, record.Content
		if err := g.store(ctx, file); err != nil && ctx.Err() != nil {
			return repositoryFile{}, ctx.Err()
		}
		return file, nil
	}
	return g.fetchSelectedFile(ctx, ref, kind, filenames, resolved.RepositoryID, commit, object, route, resolved.ObservedAt)
}

func selectResolution(ref actions.Reference, kind string, resolved references.Result, learned repositoryid.ID) (string, string, actions.Route, *availability.Unavailability) {
	invalid := availability.InvalidResolverResult()
	if resolved.Version != 1 || resolved.Error != "" || resolved.RepositoryID.Conflicts(learned) || !strings.EqualFold(resolved.Repository, ref.Repository()) || !fullSHA1(resolved.Commit) || resolved.Tree != "" || resolved.ObservedAt == "" || len(resolved.FetchLog) == 0 {
		return "", "", actions.Route{}, &invalid
	}
	route := actions.ObjectRoute()
	if kind == "action" {
		if !fullSHA1(resolved.SelectedObject) {
			return "", "", actions.Route{}, &invalid
		}
		if resolved.SelectedRef != "" || !strings.EqualFold(ref.Ref(), resolved.SelectedObject) {
			var err error
			if route, err = actions.RefRoute(resolved.SelectedRef); err != nil {
				return "", "", actions.Route{}, &invalid
			}
		}
	}
	return resolved.Commit, resolved.SelectedObject, route, nil
}

func selectedFile(ref actions.Reference, repository, kind string, id repositoryid.ID, commit, object string, route actions.Route, observed string) (repositoryFile, bool) {
	identity, err := actions.NewIdentity(ref.Host(), repository, id, commit, ref.Directory())
	if err == nil && kind != "action" {
		return repositoryFile{reference: ref, identity: identity, observedAt: observed}, true
	}
	if err == nil {
		if selection, err := actions.NewSelection(ref, identity, object, route, observed); err == nil {
			return repositoryFile{reference: ref, identity: identity, selection: selection, observedAt: observed}, true
		}
	}
	return repositoryFile{}, false
}

func (g *GitHub) fetchSelectedFile(ctx context.Context, ref actions.Reference, kind string, filenames []string, id repositoryid.ID, commit, object string, route actions.Route, observed string) (repositoryFile, error) {
	filename, content, missing, err := g.fetchRawFile(ctx, ref, commit, kind, filenames)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	file, valid := selectedFile(ref, ref.Repository(), kind, id, commit, object, route, observed)
	if !valid {
		return unavailableFile(availability.Failed()), nil
	}
	file.filename, file.content = filename, content
	if err := g.store(ctx, file); err != nil && ctx.Err() != nil {
		return repositoryFile{}, ctx.Err()
	}
	return file, nil
}

func (g *GitHub) lookupFileREST(ctx context.Context, ref actions.Reference, kind string, filenames []string) (repositoryFile, error) {
	ctx = accounting.WithRequestOptions(ctx, accounting.RequestOptions{Purpose: kind + "-metadata", Priority: accounting.Enrichment, RequestKind: accounting.REST, QuotaPool: "core"})
	if session := accounting.RequestTrackerFromContext(ctx); session != nil {
		request, err := g.request(ctx, repositoryURL(ref), false)
		if err != nil {
			return repositoryFile{}, err
		}
		if err := session.LoadGitHubRateLimits(ctx, g.client, request); err != nil {
			return repositoryFile{}, err
		}
		if missing, err := admitBatch(ctx, session, request, 1); err != nil || missing != nil {
			return unavailable(missing, err)
		}
	}
	repoID, repoName, missing, err := g.repository(ctx, ref)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	commit := strings.ToLower(ref.Ref())
	object := commit
	route := actions.ObjectRoute()
	observed := time.Now().UTC().Format(time.RFC3339Nano)
	if g.resolver != nil {
		resolved, resolveErr := g.resolver().Resolve(ctx, references.Request{Version: 1, Repository: ref.Repository(), Ref: ref.Ref(), Kind: kind, ExpectedRepositoryID: repoID})
		if resolveErr != nil {
			return unavailableError(resolveErr)
		}
		var invalid *availability.Unavailability
		if commit, object, route, invalid = selectResolution(ref, kind, resolved, repoID); invalid != nil {
			return unavailableFile(*invalid), nil
		}
		observed = resolved.ObservedAt
	}
	file, valid := selectedFile(ref, repoName, kind, repoID, commit, object, route, observed)
	if !valid {
		return unavailableFile(availability.Failed()), nil
	}
	if cached, exists, cacheErr := g.cached(ctx, ref, repoID, commit, filenames); cacheErr == nil && exists {
		file.filename, file.content = cached.Filename, cached.Content
		if err := g.storeTagObject(ctx, file); err != nil && ctx.Err() != nil {
			return repositoryFile{}, ctx.Err()
		}
		return file, nil
	}
	if session := accounting.RequestTrackerFromContext(ctx); session != nil {
		request, err := g.request(ctx, repositoryURL(ref), false)
		if err != nil {
			return repositoryFile{}, err
		}
		if missing, err := admitBatch(ctx, session, request, uint64(2+len(filenames))); err != nil || missing != nil {
			return unavailable(missing, err)
		}
	}
	objectBytes, missing, err := g.get(ctx, repositoryURL(ref)+"/git/commits/"+commit, false)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	if !strings.EqualFold(commitObjectSHA(objectBytes), commit) {
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
	afterID, afterName, missing, err := g.repository(ctx, ref)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	if afterID != repoID || !strings.EqualFold(afterName, repoName) {
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

func admitBatch(ctx context.Context, session *accounting.RequestTracker, request *http.Request, units uint64) (*availability.Unavailability, error) {
	err := session.CheckBatchQuota(request, units)
	switch {
	case err == nil:
		return nil, nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.Is(err, accounting.ErrBudget) || errors.Is(err, accounting.ErrRateLimited):
		reason := availability.Limit()
		return &reason, nil
	}
	reason := availability.Failed()
	return &reason, nil
}

func commitObjectSHA(content []byte) string {
	var object struct {
		SHA string `json:"sha"`
	}
	if json.Unmarshal(content, &object) == nil {
		return object.SHA
	}
	return ""
}

func (g *GitHub) repository(ctx context.Context, ref actions.Reference) (repositoryid.ID, string, *availability.Unavailability, error) {
	content, missing, err := g.get(ctx, repositoryURL(ref), false)
	if err != nil || missing != nil {
		return repositoryid.ID{}, "", missing, err
	}
	var repo struct {
		ID      uint64 `json:"id"`
		Name    string `json:"full_name"`
		Private *bool  `json:"private"`
	}
	if json.Unmarshal(content, &repo) == nil && strings.EqualFold(repo.Name, ref.Repository()) {
		if id, err := repositoryid.Known(repo.ID); err == nil {
			g.listings.observeVisibility(repo.Name, repo.Private)
			return id, repo.Name, nil, nil
		}
	}
	failed := availability.Failed()
	return repositoryid.ID{}, "", &failed, nil
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
	response, err := g.exchange(ctx, request, maxBody)
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
	if response.status != http.StatusOK {
		reason := availability.Failed()
		switch response.status {
		case http.StatusUnauthorized, http.StatusForbidden:
			reason = availability.Denied()
			if response.status == http.StatusForbidden && (response.header.Get("X-RateLimit-Remaining") == "0" || response.header.Get("Retry-After") != "") {
				reason = availability.Limit()
			}
		case http.StatusNotFound:
			reason = availability.Inaccessible()
		case http.StatusTooManyRequests:
			reason = availability.Limit()
		}
		return nil, &reason, nil
	}
	if len(response.body) > maxBody {
		reason := availability.Limit()
		return nil, &reason, nil
	}
	return response.body, nil, nil
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
	Version      int             `json:"version"`
	Host         string          `json:"host"`
	Repository   string          `json:"repository"`
	RepositoryID repositoryid.ID `json:"repository_id,omitzero"`
	Commit       string          `json:"commit"`
	TagObject    string          `json:"tag_object,omitzero"`
	Directory    string          `json:"directory"`
	ObservedAt   string          `json:"observed_at"`
	Filename     string          `json:"filename"`
	Content      string          `json:"content"`
	SHA256       string          `json:"sha256"`
}

const (
	cacheVersion  = 2
	cacheFilename = "metadata.json"
)

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (g *GitHub) bucket(ref actions.Reference, commit string) string {
	return filepath.Join(g.cacheDir, digest(ref.Host()+"\x00"+strings.ToLower(ref.Repository())+"\x00"+strings.ToLower(commit)+"\x00"+ref.Directory()))
}

func (g *GitHub) cached(ctx context.Context, ref actions.Reference, learned repositoryid.ID, object string, filenames []string) (cacheRecord, bool, error) {
	if g.cacheDir == "" {
		return cacheRecord{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return cacheRecord{}, false, err
	}
	reader := files.New(ctx, nil, nil)
	file, err := reader.Open(filepath.Join(g.bucket(ref, object), cacheFilename))
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
	if len(content) > maxCache || json.Unmarshal(content, &record) != nil || record.Version != cacheVersion || record.Host != ref.Host() || !strings.EqualFold(record.Repository, ref.Repository()) || record.RepositoryID.Conflicts(learned) || !record.selectedBy(object) || record.Directory != ref.Directory() || record.SHA256 != digest(record.Content) || len(record.Content) > maxBody || !slices.Contains(filenames, record.Filename) {
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

func (r cacheRecord) selectedBy(object string) bool {
	if r.TagObject == "" {
		return strings.EqualFold(r.Commit, object)
	}
	return strings.EqualFold(r.TagObject, object) && fullSHA1(r.Commit) && !strings.EqualFold(r.Commit, object)
}

func (g *GitHub) offline(ctx context.Context, ref actions.Reference, filenames []string) (repositoryFile, error) {
	if g.cacheDir == "" {
		return unavailableFile(availability.CacheMiss()), nil
	}
	record, exists, err := g.cached(ctx, ref, repositoryid.ID{}, ref.Ref(), filenames)
	if err != nil {
		if ctx.Err() != nil {
			return repositoryFile{}, ctx.Err()
		}
		return unavailableFile(availability.Failed()), nil
	}
	if !exists {
		return unavailableFile(availability.CacheMiss()), nil
	}
	file, valid := cachedRepositoryFile(ref, record)
	if !valid {
		return unavailableFile(availability.Failed()), nil
	}
	return file, nil
}

func cachedRepositoryFile(ref actions.Reference, record cacheRecord) (repositoryFile, bool) {
	identity, err := actions.NewIdentity(record.Host, record.Repository, record.RepositoryID, record.Commit, record.Directory)
	if err != nil {
		return repositoryFile{}, false
	}
	var selection actions.Selection
	if record.Filename != "" {
		if selection, err = actions.NewSelection(ref, identity, strings.ToLower(ref.Ref()), actions.ObjectRoute(), record.ObservedAt); err != nil {
			return repositoryFile{}, false
		}
	}
	return repositoryFile{reference: ref, identity: identity, selection: selection, observedAt: record.ObservedAt, filename: record.Filename, content: record.Content}, true
}

func (g *GitHub) store(ctx context.Context, manifest repositoryFile) error {
	err := g.storeAt(ctx, manifest, "")
	return errors.Join(err, g.storeTagObject(ctx, manifest))
}

func (g *GitHub) storeTagObject(ctx context.Context, manifest repositoryFile) error {
	requested := strings.ToLower(manifest.reference.Ref())
	if fullSHA1(requested) && requested != manifest.Identity().Commit() {
		return g.storeAt(ctx, manifest, requested)
	}
	return nil
}

func (g *GitHub) storeAt(ctx context.Context, manifest repositoryFile, tagObject string) error {
	if g.cacheDir == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	i := manifest.Identity()
	record := cacheRecord{Version: cacheVersion, Host: i.Host(), Repository: i.Repository(), RepositoryID: i.RepositoryID(), Commit: i.Commit(), TagObject: tagObject, Directory: i.Directory(), ObservedAt: manifest.observedAt, Filename: manifest.Filename(), Content: manifest.Content(), SHA256: digest(manifest.Content())}
	content, err := json.Marshal(record)
	if err != nil {
		return err
	}
	key := i.Commit()
	if tagObject != "" {
		key = tagObject
	}
	bucket := g.bucket(manifest.reference, key)
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
	return os.Rename(file.Name(), filepath.Join(bucket, cacheFilename))
}
