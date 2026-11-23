# GitHub Agentic Workflows

actionlint checks the `.lock.yml` files that `gh aw compile` generates.

```sh
actionlint .github/workflows/*.lock.yml
```

actionlint does not check whether a lock file matches its Markdown source. Recompile after each frontmatter change.
