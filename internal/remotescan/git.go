package remotescan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gitCommand uses only built-in operations, never checkout/archive, configured helpers or hooks.
// Credentials travel through environment config, never URLs/argv or returned subprocess output.
func (s *scanner) gitCommand(ctx context.Context, dir string, authenticated bool, args ...string) *exec.Cmd {
	config := []string{"-c", "core.hooksPath=" + filepath.Join(s.temp, "no-hooks"), "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "credential.interactive=false", "-c", "http.followRedirects=false", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always"}
	cmd := exec.CommandContext(ctx, "git", append(config, args...)...)
	configureProcess(cmd)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if (strings.HasPrefix(upper, "GIT_") && upper != "GIT_SSL_CAINFO" && upper != "GIT_SSL_CAPATH") || upper == "GH_TOKEN" || upper == "GITHUB_TOKEN" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GIT_NO_REPLACE_OBJECTS=1", "GIT_SSH_COMMAND=ssh -oBatchMode=yes -oStrictHostKeyChecking=yes")
	if authenticated && s.opts.Token != "" {
		header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+s.opts.Token))
		cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http."+strings.TrimRight(s.opts.ServerURL, "/")+"/.extraHeader", "GIT_CONFIG_VALUE_0="+header)
	}
	cmd.Stderr = io.Discard
	return cmd
}

type limitedBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(len(p)) > b.limit-int64(b.Len()) {
		return 0, errors.New("Git output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func (s *scanner) git(ctx context.Context, dir string, authenticated bool, limit int64, args ...string) ([]byte, error) {
	cmd := s.gitCommand(ctx, dir, authenticated, args...)
	output := limitedBuffer{limit: limit}
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("Git command failed (details withheld to protect credentials)")
	}
	return output.Bytes(), nil
}

func (s *scanner) matchesLocal(ctx context.Context) bool {
	data, err := s.git(ctx, s.opts.Local, false, 8192, "config", "--get", "remote.origin.url")
	if err != nil {
		return false
	}
	remote := strings.TrimSpace(string(data))
	server, err := url.Parse(s.opts.ServerURL)
	if err != nil {
		return false
	}
	if strings.HasPrefix(remote, "git@") {
		host, repo, ok := strings.Cut(strings.TrimPrefix(remote, "git@"), ":")
		return ok && strings.EqualFold(host, server.Hostname()) && strings.TrimSuffix(repo, ".git") == s.opts.Repository
	}
	u, err := url.Parse(remote)
	if err != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "ssh" {
		return u.User != nil && u.User.String() == "git" && strings.EqualFold(u.Hostname(), server.Hostname()) && strings.TrimSuffix(u.Path, ".git") == "/"+s.opts.Repository
	}
	return u.Scheme == server.Scheme && u.User == nil && strings.EqualFold(u.Host, server.Host) && strings.TrimSuffix(u.Path, ".git") == strings.TrimRight(server.Path, "/")+"/"+s.opts.Repository
}

func (s *scanner) resolveLocal(ctx context.Context, dir, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	data, err := s.git(ctx, dir, false, 128, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(data))
	if !shaPattern.MatchString(sha) {
		return "", errors.New("Git did not return a commit SHA")
	}
	return strings.ToLower(sha), nil
}

func (s *scanner) fetchFallback(ctx context.Context, ref string) (string, string, error) {
	server, err := url.Parse(s.opts.ServerURL)
	if err != nil {
		return "", "", err
	}
	sshURL := s.opts.SSHURL
	if sshURL == "" {
		host := server.Hostname()
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		sshURL = "ssh://git@" + host + "/" + s.opts.Repository + ".git"
	}
	remotes := []struct {
		name, url     string
		authenticated bool
	}{
		{"git-https", strings.TrimRight(s.opts.ServerURL, "/") + "/" + s.opts.Repository + ".git", true},
		{"git-ssh", sshURL, false},
	}
	for _, remote := range remotes {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		dir, err := os.MkdirTemp(s.temp, "git-")
		if err != nil {
			return "", "", err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err = s.git(attemptCtx, s.temp, false, 4096, "init", "--bare", "--template=", dir)
		if err == nil {
			if ref == "" {
				ref = "HEAD"
			}
			err = s.fetchGit(attemptCtx, dir, remote.url, ref, remote.authenticated)
		}
		sha := ""
		if err == nil {
			sha, err = s.resolveLocal(attemptCtx, dir, "FETCH_HEAD")
		}
		cancel()
		s.attempt(remote.name, err)
		var invalid *snapshotError
		if errors.As(err, &invalid) {
			return "", "", err
		}
		if err == nil {
			s.result.Source.Transport = remote.name
			return dir, sha, nil
		}
	}
	return "", "", errors.New("repository/ref unavailable through configured transports; see attempts")
}

type treeEntry struct{ name, sha string }

func (s *scanner) materializeGit(ctx context.Context, dir, sha string) (string, error) {
	resolved, err := s.resolveLocal(ctx, dir, sha)
	if err != nil {
		return "", err
	}
	if resolved != sha {
		return "", unsafeSnapshot("Git object is not the requested commit")
	}
	data, err := s.git(ctx, dir, false, int64(s.opts.MaxFiles)*4096, "ls-tree", "-r", "-z", "--full-tree", sha)
	if err != nil {
		return "", err
	}
	entries := []treeEntry{}
	for _, record := range bytes.Split(data, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		header, name, ok := strings.Cut(string(record), "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || !shaPattern.MatchString(fields[2]) {
			return "", unsafeSnapshot("invalid Git tree entry")
		}
		if fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			return "", unsafeSnapshot("Git tree contains links or submodules")
		}
		if !safePath(name) {
			return "", unsafeSnapshot("unsafe Git tree path")
		}
		entries = append(entries, treeEntry{name, fields[2]})
		if len(entries) > s.opts.MaxFiles {
			return "", unsafeSnapshot("Git tree exceeds file limit")
		}
	}
	root, err := os.MkdirTemp(s.temp, "snapshot-")
	if err != nil {
		return "", err
	}
	cmd := s.gitCommand(ctx, dir, false, "cat-file", "--batch")
	var queries strings.Builder
	for _, entry := range entries {
		queries.WriteString(entry.sha + "\n")
	}
	cmd.Stdin = strings.NewReader(queries.String())
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", errors.New("could not start Git blob reader")
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	reader := bufio.NewReader(pipe)
	writer := snapshotWriter{root: root, opts: s.opts, names: map[string]string{}}
	for _, entry := range entries {
		line, err := reader.ReadString('\n')
		if err != nil || len(line) > 128 {
			return "", unsafeSnapshot("invalid Git blob header")
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != entry.sha || fields[1] != "blob" {
			return "", unsafeSnapshot("Git returned an unexpected blob")
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return "", unsafeSnapshot("invalid Git blob size")
		}
		if err := writer.write(ctx, entry.name, size, reader); err != nil {
			return "", err
		}
		separator, err := reader.ReadByte()
		if err != nil || separator != '\n' {
			return "", unsafeSnapshot("invalid Git blob trailer")
		}
	}
	if err := cmd.Wait(); err != nil {
		waited = true
		return "", errors.New("Git blob reader failed")
	}
	waited = true
	return root, nil
}

// Git's protocol does not expose a download byte cap. Monitor its private object directory and
// cancel when it passes the snapshot budget; the polling interval permits a bounded-time overshoot.
func (s *scanner) fetchGit(ctx context.Context, dir, remote, ref string, authenticated bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.git(ctx, dir, authenticated, 4096, "-c", "fetch.fsckObjects=true", "-c", "transfer.fsckObjects=true", "fetch", "--no-tags", "--depth=1", "--no-recurse-submodules", "--", remote, ref)
		done <- err
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				return err
			}
			return gitDiskBudget(dir, s.opts.MaxBytes)
		case <-ticker.C:
			if err := gitDiskBudget(dir, s.opts.MaxBytes); err != nil {
				cancel()
				<-done
				return err
			}
		}
	}
}

func gitDiskBudget(dir string, limit int64) error {
	var size int64
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		size += info.Size()
		if size > limit {
			return unsafeSnapshot("Git object storage exceeds byte limit")
		}
		return nil
	})
}
