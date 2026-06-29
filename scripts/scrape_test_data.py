#!/usr/bin/env python3
"""Build test_data/ fixtures for the template engine + settings test UI.

Fetches REAL captured webhook payloads, wraps each in our event envelope
({event_type, linear|github: raw}), and sanitizes PII to obvious placeholders.
The output files are committed and used both as Go test fixtures and as the
sample-event dropdown in the Linear settings UI.

Sources (fetched via the authenticated `gh` CLI):
  - GitHub: octokit/webhooks payload-examples (official, machine-generated).
  - Linear: real captured Issue deliveries from a public webhook log.

Idempotent: re-running regenerates test_data/ from the same sources. The
captured Linear log has no `remove` Issue event, so one is derived from an
`update` payload (only `action` is changed) and marked as derived.

Usage:  python3 scripts/scrape_test_data.py
Requires: gh (authenticated), python3.
"""
from __future__ import annotations

import base64
import json
import re
import subprocess
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
OUT = REPO_ROOT / "test_data"

# (octokit event dir, action) -> output filename stem
GITHUB_PAYLOADS = [
    ("issues", "opened"),
    ("issues", "labeled"),
    ("issues", "reopened"),
    ("pull_request", "opened"),
    ("pull_request", "closed"),
    ("pull_request", "labeled"),
]

# Captured Linear Issue deliveries (filename in the source log) -> output stem.
LINEAR_PAYLOADS = {
    "1776521193567-923ec00c-Issue.json": "issue.created",
    "1776515043385-574fbd25-Issue.json": "issue.status_changed",  # -> Done, has updatedFrom
}
LINEAR_SRC_REPO = "skogai/claude-backup2"
LINEAR_SRC_DIR = "logs/linear-notifications"


def gh_file(repo: str, path: str) -> bytes:
    """Fetch a file's raw bytes from a GitHub repo via the gh CLI."""
    out = subprocess.run(
        ["gh", "api", f"repos/{repo}/contents/{path}", "--jq", ".content"],
        check=True, capture_output=True, text=True,
    ).stdout
    return base64.b64decode(out)


# --- PII sanitization --------------------------------------------------------
# Keep structure + business fields (identifier, state, labels); replace people
# and org/account identifiers with stable, obvious placeholders.
EMAIL_RE = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")


def sanitize(obj):
    """Recursively replace PII-ish values with placeholders, in place."""
    if isinstance(obj, dict):
        for k, v in list(obj.items()):
            kl = k.lower()
            if kl in ("name", "displayname", "full_name") and isinstance(v, str) and v:
                # Avoid clobbering names that are clearly not people (e.g. repo
                # "name", label "name", state "name", team "name"): only scrub
                # actor/user/assignee/creator/owner/sender blocks (handled by
                # parent context below). Here, leave generic names alone.
                obj[k] = v
            elif kl == "email" and isinstance(v, str):
                obj[k] = "user@example.com"
            elif isinstance(v, str):
                obj[k] = EMAIL_RE.sub("user@example.com", v)
            else:
                sanitize(v)
    elif isinstance(obj, list):
        for item in obj:
            sanitize(item)


# Person-ish containers whose name/login should be replaced wholesale.
PERSON_KEYS = {"actor", "user", "assignee", "creator", "owner", "sender", "author"}
PLACEHOLDER_NAME = "Ada Lovelace"
PLACEHOLDER_LOGIN = "ada-sample"


def scrub_people(obj):
    """Replace name/login/email inside person-ish sub-objects."""
    if isinstance(obj, dict):
        for k, v in obj.items():
            if k in PERSON_KEYS and isinstance(v, dict):
                if "name" in v and isinstance(v["name"], str):
                    v["name"] = PLACEHOLDER_NAME
                if "login" in v and isinstance(v["login"], str):
                    v["login"] = PLACEHOLDER_LOGIN
                if "email" in v and isinstance(v["email"], str):
                    v["email"] = "ada@example.com"
            scrub_people(v)
    elif isinstance(obj, list):
        for item in obj:
            scrub_people(item)


def finalize(raw: dict) -> dict:
    scrub_people(raw)
    sanitize(raw)
    return raw


def write(path: Path, envelope: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(envelope, indent=2, sort_keys=True) + "\n")
    print(f"wrote {path.relative_to(REPO_ROOT)}")


def main() -> None:
    # GitHub
    for evt, act in GITHUB_PAYLOADS:
        raw = json.loads(gh_file("octokit/webhooks",
                                 f"payload-examples/api.github.com/{evt}/{act}.payload.json"))
        write(OUT / "github" / f"{evt}.{act}.json",
              {"event_type": "github", "github": finalize(raw)})

    # Linear (captured) — payload is under a "payload" key in the log file.
    status_changed_raw = None
    for src, stem in LINEAR_PAYLOADS.items():
        log = json.loads(gh_file(LINEAR_SRC_REPO, f"{LINEAR_SRC_DIR}/{src}"))
        raw = log["payload"]
        if stem == "issue.status_changed":
            status_changed_raw = json.loads(json.dumps(raw))  # deep copy for derive
        write(OUT / "linear" / f"{stem}.json",
              {"event_type": "linear", "linear": finalize(raw)})

    # Derive a `remove` from the status_changed payload (the log had no real
    # remove Issue event). Only `action` changes; shape is identical.
    if status_changed_raw is not None:
        status_changed_raw["action"] = "remove"
        write(OUT / "linear" / "issue.removed.json",
              {"event_type": "linear", "linear": finalize(status_changed_raw)})


if __name__ == "__main__":
    main()
