# Review Instructions

## Important Findings

Mark a finding Important only when the PR likely introduces a real behavior bug,
data loss, broken automation, incompatible CLI/API behavior, broken persistence,
incorrect configuration handling, authentication or secret-handling risk, or
release packaging breakage.

## Nit Findings

Use Nit for small maintainability issues only when they are clearly introduced by
the PR. Do not post more than five Nit comments. If there are more, summarize the
extra items in the final comment instead of posting each one inline.

## Do Not Report

- Formatting-only concerns unless they make the code hard to read.
- Pre-existing issues outside the PR diff unless the PR makes them worse.
- Speculative risks without a concrete code path and file/line evidence.
- Missing tests by itself unless the missing coverage hides a likely behavior bug.

## Final Comment

Always post one top-level PR comment. Start with either `No blocking issues
found.` or `Blocking issues found.` Then summarize the reviewed areas and list
any Important findings first.
