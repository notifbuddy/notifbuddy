---
title: Templates
description: Expression syntax for channel names and creation conditions.
---

Channel names and creation conditions use **GitHub Actions expression syntax**, run against the incoming event.

```
${{ linear.data.identifier }}          # channel name
linear.data.state.name == 'Done'       # condition
```

## Event data

Reference fields off the event envelope:

- `event_type` — `linear` or `github`
- `linear.*` / `github.*` — the raw event payload

Example: `linear.data.state.name`, `linear.action`, `github.repository`.

## Operators

`==` `!=` `<` `<=` `>` `>=` · `&&` `||` `!` · `( )` · `.` `[ ]` · `.*` (object filter)

Literals: strings (`'...'`), numbers, `true`, `false`, `null`.

## Functions

| Function | Does |
| --- | --- |
| `contains(a, b)` | Substring, or array membership |
| `startsWith(str, prefix)` | Prefix check |
| `endsWith(str, suffix)` | Suffix check |
| `format(fmt, ...)` | `{0}`, `{1}` placeholders |
| `join(array, sep?)` | Join array (default `,`) |
| `toJSON(value)` | Serialize to JSON |
| `fromJSON(str)` | Parse JSON |

String functions are case-insensitive. Names are case-insensitive.

## Not supported

CI-only functions error if used: `hashFiles`, `success`, `always`, `cancelled`, `failure`.

## Examples

```
# only when moved to Done
linear.data.state.name == 'Done'

# bugs only
contains(join(linear.data.labels.*.name), 'bug')

# name by identifier
tkt-${{ linear.data.identifier }}
```
