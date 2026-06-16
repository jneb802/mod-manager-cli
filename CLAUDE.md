# Repository Instructions

## Review Context

- Review pull requests for behavior bugs introduced by the diff.
- Prefer the repository's existing architecture, naming, and helper APIs.
- Treat CLI behavior, user-facing output, configuration, file paths, persistence, network calls, and release automation as important compatibility surfaces.
- Keep changes scoped to the requested behavior.
- Do not print secrets or raw tokens.

## Review Expectations

- Cite specific files and lines for any finding.
- Do not flag style-only issues unless they obscure behavior or maintainability.
- Do not report pre-existing issues unless the PR makes them worse.
- If a full build or integration test needs unavailable secrets, services, SDKs, or private dependencies, say so instead of asking for an impossible runner check.
