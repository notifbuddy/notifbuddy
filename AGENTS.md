# notifbuddy agent rules

Rules for any coding agent working in this repo. Mirrors `.cursor/rules/`, which
Cursor loads on its own — keep the two in sync when either changes.

## No comments

Never add comments to code you write or edit.

- Do not add new comments (line, block, docstrings, or JSDoc/godoc-style).
- Do not narrate what the code does in comments.
- Do not leave TODOs, section banners, or explanatory headers in code.
- Prefer clear names and structure over commentary.
- Only touch an existing comment if you must update or remove it because the surrounding code changed and the comment would become wrong — do not expand it.
- Exception: language or toolchain requirements that are not prose (e.g. a required `//go:generate` directive, a license header the file already has, or an unavoidable linter directive like `//nolint` with a real need).

Explanation belongs in the commit message and the pull request, not in the source.
