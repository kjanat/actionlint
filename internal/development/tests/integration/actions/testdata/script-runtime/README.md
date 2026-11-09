The action files are unchanged copies of the public
`kjanat/gpg-signing-service/.github/actions/check-api-changes/action.yml`.

- `gpg-881bd5cd.action.yml` comes from revision
  `881bd5cd17f04bf8b1a3cf13f5b81fa2c8ef1e72`. It contains the failure-status
  capture and malformed notice reported in actionlint PR343 comment 6713776814.
- `gpg-ac6e230b.action.yml` comes from revision
  `ac6e230b54350c8099210ca59f1b5b7ee0d027c3`, the fixed revision cited by that
  comment.

`TestScriptRuntimePublicGPGAction` analyzes these files and checks the exact
findings and declaring lines. It never executes the action commands.
