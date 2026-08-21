package main

import (
	"fmt"
	"regexp"
)

type rule struct {
	desc    string
	pattern *regexp.Regexp
	count   int
}

type target struct {
	path      string
	rules     []rule
	unrelated []string
}

func mustRule(desc, pattern string, count int) rule {
	re := regexp.MustCompile(pattern)
	if re.NumSubexp() != 1 {
		panic(fmt.Sprintf("rule %q must capture the version with exactly one group but has %d", desc, re.NumSubexp()))
	}
	if count < 1 {
		panic(fmt.Sprintf("rule %q must expect at least one occurrence", desc))
	}
	return rule{desc: desc, pattern: re, count: count}
}

var targets = []*target{
	{
		path: ".pre-commit-hooks.yaml",
		rules: []rule{
			mustRule("pre-commit Docker image tag", `(?m)^  entry: ghcr\.io/kjanat/actionlint:(\d+\.\d+\.\d+)\r?$`, 1),
		},
		unrelated: []string{"minimum_pre_commit_version: 3.0.0"},
	},
	{
		path: "action.yml",
		rules: []rule{
			mustRule("action Docker image tag", `image: docker://ghcr\.io/kjanat/actionlint:action-(\d+\.\d+\.\d+)`, 1),
		},
	},
	{
		path: "scripts/download-actionlint.bash",
		rules: []rule{
			mustRule("default version of the download script", `(?m)^version="(\d+\.\d+\.\d+)"\r?$`, 1),
		},
		unrelated: []string{
			"1.6.9",
			"releases/download/v1.0.0/actionlint_1.0.0_linux_386.tar.gz",
		},
	},
	{
		path: "docs/usage.md",
		rules: []rule{
			mustRule("versioned release tag note", "`v(\\d+\\.\\d+\\.\\d+)` is a versioned release tag", 1),
			mustRule("download script argument", `download-actionlint\.bash\) (\d+\.\d+\.\d+)`, 3),
			mustRule("CLI image tag example", "`ghcr\\.io/kjanat/actionlint:(\\d+\\.\\d+\\.\\d+)`", 1),
			mustRule("action image tag example", "`action-(\\d+\\.\\d+\\.\\d+)`", 1),
			mustRule("pre-commit revision", `(?m)^    rev: v(\d+\.\d+\.\d+)\r?$`, 2),
			mustRule("Trunk linter version", ` actionlint@(\d+\.\d+\.\d+)`, 2),
		},
		unrelated: []string{
			"sarif/v2.1.0/sarif-v2.1.0.html",
			"github.com/wasilibs/go-shellcheck/cmd/shellcheck@v0.11.1",
		},
	},
	{
		path: "docs/install.md",
		rules: []rule{
			mustRule("gh release download tag", `--pattern '[^']+' v(\d+\.\d+\.\d+)`, 1),
			mustRule("release asset name", `actionlint_(\d+\.\d+\.\d+)_linux_amd64\.tar\.gz`, 2),
			mustRule("download script example description", `example installs v(\d+\.\d+\.\d+)`, 1),
			mustRule("download script argument", `download-actionlint\.bash\) (\d+\.\d+\.\d+)`, 1),
		},
		unrelated: []string{"actionlint v1.7.11"},
	},
	{
		path: "README.md",
		rules: []rule{
			mustRule("versioned release tag note", "`v(\\d+\\.\\d+\\.\\d+)` is a versioned release tag", 1),
			mustRule("document link", `/blob/v(\d+\.\d+\.\d+)/docs/`, 6),
		},
	},
	{
		path: "man/actionlint.1.md",
		rules: []rule{
			mustRule("manual footer version", `(?m)^footer: actionlint (\d+\.\d+\.\d+)\r?$`, 1),
			mustRule("document link", `/blob/v(\d+\.\d+\.\d+)/docs/`, 6),
		},
	},
	{
		path: "playground/index.html",
		rules: []rule{
			mustRule("release page link", `releases/tag/v(\d+\.\d+\.\d+)`, 1),
			mustRule("version badge", `id="version">v(\d+\.\d+\.\d+)`, 1),
			mustRule("document link", `/blob/v(\d+\.\d+\.\d+)/docs/`, 1),
		},
	},
}
