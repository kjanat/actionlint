# Runner runtime source fixtures

These files are unchanged copies from the public `actions/runner` repository.
The directory names identify their complete upstream revisions.
The source files use GitHub's MIT license, copied into `LICENSE.txt`.

- At `4fbf7ca776674610496d3d637d48393bd98b9cf2`, the parsers accept Node 12, 16, 20 and 24.
  The package contains Node 20 and 24, with migration constants that deprecate
  Node 20 and describe its scheduled removal.
- `77e8fff894e18527cddd3684b015475c3ca01532` is the inspected runner `main`
  revision after commit `cf65f3ec8100ee0046d735ac5d647e00f4b8e817`,
  `chore(runner): remove bundled node20 (#4836)`. The parsers accept the same
  metadata values. Packaging contains only Node 24, and `Constants.cs` has no
  `NodeMigration` class.

The upstream paths are `src/Runner.Worker/ActionManifestManager.cs`,
`src/Runner.Worker/ActionManifestManagerLegacy.cs`, `src/Misc/externals.sh` and
`src/Runner.Common/Constants.cs`, and `src/Runner.Worker/Handlers/HandlerFactory.cs`.
Each directory contains all five files from
the named revision.

`TestRuntimeLifecycleFromRunnerSources` checks accepted runtime names, packaged
executables and migration constants from both revisions. It also verifies that
generated output identifies its source revision. Separate execution tests check
the replacement runtime selected by `HandlerFactory.cs`.
Separate synthetic cases check future runtime discovery and reject unsupported
migration declarations or missing source files. The tests read these files as text.
