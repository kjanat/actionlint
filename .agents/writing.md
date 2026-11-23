# Writing instructions

Use the real names of programs, commands and settings. Check the code or specification before describing its behavior. Replace vague labels with the operation they describe. If you list options, say when to use each one. Delete contrasts, rebuttals and sentences that add no information.

Replace inflated wording with familiar words. Split sentences that combine several explanations. Correct smart quotes and hidden whitespace in prose. Preserve quotations and code examples.

## Explain a difference that affects the result

```yaml
env:
  CONFIG: '{target:"linux"}'
```

```text
env.CONFIG == env.CONFIG
→ true

fromJSON(env.CONFIG) == fromJSON(env.CONFIG)
→ false

fromJSON(env.CONFIG).target == 'linux'
→ true
```

Every `fromJSON()` call creates a new copy. GitHub treats two copies as different even when their fields match. To run a step only for Linux, compare `.target` with `'linux'`.
