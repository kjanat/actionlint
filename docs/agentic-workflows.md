# GitHub Agentic Workflows

GitHub Agentic Workflows (`gh-aw`) uses Markdown files with YAML frontmatter
and natural-language instructions. `gh aw compile` generates the executable
`.github/workflows/*.lock.yml` files. actionlint analyzes the compiled YAML as
GitHub Actions workflows; it does not parse the `.md` source or certify what an
agent prompt means or how an agent behaves.

Run actionlint on the generated workflows with the usual command:

```sh
actionlint .github/workflows/*.lock.yml
```

The `gh-aw` compiler can also invoke actionlint during compilation:

```sh
gh aw compile --actionlint
```

Use the compiler to regenerate lock files after frontmatter changes, then lint
the generated YAML. Keep the Markdown source and compiled lock together. Review
permissions, generated job dependencies, action pins, and runtime expressions
in the lock file. actionlint checks Actions syntax and its configured static
rules; it does not validate gh-aw compiler freshness, prompt intent, sandbox
behavior, or runtime security guarantees. Follow the gh-aw compiler and
security guidance for those checks.

References: [gh-aw compilation process](https://github.github.com/gh-aw/reference/compilation-process/),
[workflow linters](https://github.github.com/gh-aw/reference/workflow-linters/),
[workflow structure](https://github.github.com/gh-aw/reference/workflow-structure/).
