# Security Policy

## Supported Versions

Security fixes ship as a patch release of the latest minor version. The floating `v1` Action tag and the package managers listed in [the installation document] follow that release.

| Version                        | Supported          |
| ------------------------------ | ------------------ |
| [Latest minor release][latest] | :white_check_mark: |
| Older releases                 | :x:                |

Versions 1.7.x and earlier are releases of [the upstream project]. Report vulnerabilities in those to [rhysd/actionlint].

## Reporting a Vulnerability

Report a vulnerability privately through [GitHub's private vulnerability reporting][report] or by [email](mailto:security+actionlint@kajkowalski.nl). Please include the affected version, how you installed it, and a workflow, configuration or command line that reproduces the problem.

I try to acknowledge reports within 7 days and keep you updated on the advisory until it is resolved. An accepted report gets a fix in a new release and a published GitHub security advisory, with credit to you unless you ask otherwise. A declined report gets an explanation in the advisory thread.

A check that misses an insecure pattern in a workflow, such as an unflagged script injection, is a bug in actionlint's analysis. Report it as a regular [issue].

[issue]: https://github.com/kjanat/actionlint/issues/new
[latest]: https://github.com/kjanat/actionlint/releases/latest
[report]: https://github.com/kjanat/actionlint/security/advisories/new
[rhysd/actionlint]: https://github.com/rhysd/actionlint/security
[the installation document]: docs/install.md
[the upstream project]: https://github.com/rhysd/actionlint
