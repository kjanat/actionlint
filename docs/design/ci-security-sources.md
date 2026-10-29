# CI security sources and rule mapping

This research baseline separates published requirements, platform guidance and
local policy. The mappings below identify research targets; they do not certify a
pipeline or claim that an existing rule implements an entire control.

## Sources

Reviewed 2026-10-27. Record a source revision and section for each implemented
mapping; rolling documentation needs a captured revision before rule acceptance.

| Source                                                                                        | Role                                                                                             | Application to actionlint                                                                                                                           |
| --------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| [GitHub Actions secure use](https://docs.github.com/en/actions/reference/security/secure-use) | Platform guidance on token permissions, untrusted code, script injection and third-party actions | Concrete workflow predicates and remediation; distinguish documented recommendations from runtime requirements.                                     |
| [OpenSSF Scorecard checks](https://github.com/ossf/scorecard/blob/main/docs/checks.md)        | Operational checks including Dangerous-Workflow, Pinned-Dependencies and Token-Permissions       | Compare check coverage and exceptions; a partial YAML check must not inherit the complete Scorecard result.                                         |
| [SLSA 1.2](https://slsa.dev/spec/v1.2/)                                                       | Versioned source/build tracks and provenance requirements                                        | Separate declared workflow intent from verified artifacts, attestations and builder guarantees. Workflow syntax cannot establish a SLSA level.      |
| [OWASP CI/CD Top 10](https://owasp.org/projects/top-10-cicd-security-risks)                   | Threat taxonomy, including dependency abuse, poisoned pipelines and credential hygiene           | Group related findings by threat; taxonomy membership is not a compliance verdict.                                                                  |
| [NIST SSDF 1.1, SP 800-218](https://csrc.nist.gov/pubs/sp/800/218/final)                      | Secure-development practice framework                                                            | Research process-level mappings separately from workflow predicates. Repository settings and organizational evidence are outside a local YAML scan. |

## Initial coverage map

Existing rule names come from [`rulecatalog`](../../internal/rulecatalog/catalog.go).
These are proposed relationships requiring a predicate-level review, not new
rule selections or implemented standards profiles.

| Proposed category                            | Existing starting point                                                                      | Required coverage work                                                                                                                                                |
| -------------------------------------------- | -------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `security/dependencies/immutable-references` | `require-commit-hash`; GitHub third-party actions guidance and Scorecard Pinned-Dependencies | Verify actions, reusable workflows and containers separately; immutable reference syntax alone does not establish trusted origin or artifact integrity.               |
| `security/credentials/token-permissions`     | `permissions`, `require-permissions`; GitHub least privilege and Scorecard Token-Permissions | Valid scope names and explicit declarations do not prove minimal privilege. Model effective permissions, inheritance and job needs before reporting excess privilege. |
| `security/execution/untrusted-inputs`        | `script-injection`; GitHub script-injection guidance and Scorecard Dangerous-Workflow        | Record supported sources, sinks, environment indirection and sanitization. Test safe patterns and unknown dataflow explicitly.                                        |
| `security/execution/privileged-checkout`     | GitHub untrusted checkout guidance; OWASP poisoned pipeline execution                        | Trace trigger, checkout ref, credentials and execution together. A trigger name alone is insufficient.                                                                |
| `security/cache/trust-boundaries`            | `cache-write-untrusted`, `cache-call-unrestricted`                                           | Review exact cache semantics and trust transitions against platform sources. Do not assign a standard identifier based only on a similar title.                       |
| `security/artifacts/provenance`              | SLSA build track; OWASP artifact integrity                                                   | Inspect actual attestations and verification policy through a provider. A signing action appearing in YAML is insufficient.                                           |

## Mapping contract

Keep rule identity stable. A rule can map to several versioned external controls;
store these relationships separately from rule names and the existing category
and subcategory. A mapping needs:

- Publisher, document version/revision, section identifier and source URL.
- Requirement kind: mandatory requirement, recommendation or project policy.
- Exact predicate, supported workflow contexts and explicit exclusions.
- Required evidence: source syntax, resolved metadata, repository settings,
  runtime observation or verified artifact.
- Coverage state: supported, partial, unavailable or not applicable; missing
  evidence cannot produce a passing control result.
- Positive, negative and unknown-input fixtures, with a rationale linking each
  expectation to the cited section.

Source acquisition belongs behind providers. Remote metadata is enabled by
default with explicit opt-out, following the
[provider direction](https://github.com/kjanat/actionlint/issues/157#issuecomment-6353123385).
Cache immutable identities and provenance; report unavailable facts when offline
or denied access. Do not infer permission to execute downloaded actions from
permission to read their metadata.

Begin with immutable references, token permissions and script injection. Review
their actual predicates before adding source mappings to the public rule contract.
Add standards profiles only after coverage and unknown-result reporting exist;
a profile must expose partial evaluation rather than turn missing controls green.
