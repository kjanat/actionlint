# Writing instructions

Follow [ISO 24495-1:2023](https://www.iso.org/standard/78907.html). Write for the reader's task. Use the real names of programs, commands and settings. Check the code or specification before describing its behavior.

Explain what happens and how to use it. Replace vague labels with the operation they describe. Explain when an instruction applies. If you list options, explain when to use each one.

Remove pointless contrasts and unsolicited rebuttals. Rewrite unclear explanations. Delete sentences that add no information. A technically correct statement can still be irrelevant.

## Rewrite an unclear explanation

Before:

> `--no-ignore` disables Git-ignore filtering, not config-file filtering.

After:

> To check Git-ignored workflows, run `actionlint check --no-ignore .github/workflows/`.

## Delete an extra claim

Before:

> `--output github` writes GitHub annotations. No custom parser is required.

After:

> `--output github` writes GitHub annotations.

## Delete empty commentary

Before:

> Run `actionlint check`. This is not just a convenience. It is an important part of your workflow.

After:

> Run `actionlint check`.

## Explain a difference that affects the result

Suppose your workflow chooses a build target from this value:

```yaml
env:
  CONFIG: '{target:"linux"}'
```

These expressions produce different results:

```text
env.CONFIG == env.CONFIG
→ true

fromJSON(env.CONFIG) == fromJSON(env.CONFIG)
→ false

fromJSON(env.CONFIG).target == 'linux'
→ true
```

Every `fromJSON()` call creates a new copy of the settings. GitHub treats those copies as different, even when all their fields match. To run a step only for Linux, compare `.target` with `'linux'`.

## Review the whole passage

Read the surrounding text before editing. A comparison helps when readers are choosing between options or need to understand why results differ. Explain that difference with an example.

Delete introductions such as "It is important to note" when the next sentence already makes the point. Replace inflated wording with familiar words. Split sentences that combine several explanations. Correct accidental smart quotes and hidden whitespace in prose. Preserve quotations and code examples.

Read comments together with their code. Delete comments that repeat what the next statement already says. Explain a surprising result or a reason the reader needs to understand the code. Comment Cop also flags long comments, so review what each sentence contributes.

## Resolve Comment Cop comments

Comment Cop's suggestions are advisory. A human may resolve a false positive. An agent may resolve a false positive only after replying with the reason. Fix other findings. Comment Cop resolves its thread when a later scan stops reporting the passage.
