package git

import (
	"os"
	"sync"

	"golang.org/x/sys/execabs"
)

// executableLookup pins a successful absolute path until the lookup environment
// changes or that file disappears. It never caches a failed search. PATH order
// and Windows PATHEXT selection are supplied by execabs, including its refusal
// to select an executable through an implicit current-directory search.
type executableLookup struct {
	mu          sync.Mutex
	environment lookupEnvironment
	path        string
}

type lookupEnvironment struct {
	path, extensions, directory, goDebug string
	noCurrentDirectory                   bool
}

func (lookup *executableLookup) resolve(find func(string) (string, error)) (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return find("git")
	}
	_, noCurrentDirectory := os.LookupEnv("NoDefaultCurrentDirectoryInExePath")
	environment := lookupEnvironment{
		path: os.Getenv("PATH"), extensions: os.Getenv("PATHEXT"), directory: directory,
		goDebug: os.Getenv("GODEBUG"), noCurrentDirectory: noCurrentDirectory,
	}
	lookup.mu.Lock()
	defer lookup.mu.Unlock()
	if lookup.path != "" && lookup.environment == environment {
		if info, err := os.Stat(lookup.path); err == nil && !info.IsDir() {
			return lookup.path, nil
		}
	}
	lookup.path = ""
	path, err := find("git")
	if err != nil {
		return "", err
	}
	lookup.environment, lookup.path = environment, path
	return path, nil
}

var gitExecutable executableLookup

func executable() (string, error) {
	return gitExecutable.resolve(execabs.LookPath)
}
