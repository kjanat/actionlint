`TestProjectWorkflowFixtures` in `fixtures_test.go` checks the projects in this directory.

Each subdirectory represents one project. Its neighboring `<project>.out` file lists the expected diagnostics, one per
line, sorted by file path and source position. An empty file means the check should report no diagnostics.

For each fixture, this test scans `<project>/workflows/`. It reads local actions
referenced by those workflows from the paths in their `uses:` declarations.

The test discovers workflows and reads configuration relative to each project directory. Paths in `.out` files are also
relative to that directory.

```
├── some_project
│   ├── actionlint.yaml
│   ├── action1
│   │   └── action.yaml
│   ├── action2
│   │   └── action.yaml
│   └── workflows
│       ├── workflow1.yaml
│       └── workflow2.yaml
└── some_project.out
```
