# Security policy

## Supported versions

I publish security fixes in a patch release of the latest minor version. Use the actionlint GitHub Action tag for that major version, such as `v2` for v2 releases. The package managers listed in [the installation document] also provide the latest release.

| Version                        | Supported |
| ------------------------------ | --------- |
| [Latest minor release][latest] | Yes       |
| Older releases                 | No        |

Versions 1.7.x and earlier are releases of [the upstream project]. Report vulnerabilities in those to [rhysd/actionlint].

## Reporting a vulnerability

Report a vulnerability privately through [GitHub's private vulnerability reporting][report] or by [email](mailto:security+actionlint@kajkowalski.nl). Please include the affected version, how you installed it, and a workflow, configuration or command line that reproduces the problem.

I try to acknowledge reports within 7 days and post updates in the advisory thread until the report is resolved. If I accept a report, I publish a fix in a new release and a GitHub security advisory. I credit the reporter unless they ask otherwise. If I decline a report, I explain why in the advisory thread.

If actionlint misses an insecure workflow pattern, such as a script injection, report the missed finding as a regular [issue].

[issue]: https://github.com/kjanat/actionlint/issues/new
[latest]: https://github.com/kjanat/actionlint/releases/latest
[report]: https://github.com/kjanat/actionlint/security/advisories/new
[rhysd/actionlint]: https://github.com/rhysd/actionlint/security
[the installation document]: docs/install.md
[the upstream project]: https://github.com/rhysd/actionlint
