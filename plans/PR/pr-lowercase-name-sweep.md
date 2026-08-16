## Summary

- Sweeps every remaining `lazyKoder` reference to the canonical lowercase product name `lazykoder` (docs, chat chrome strings, tests, configs) so the brand is consistent repo-wide after the repo/URL rename.
- Drops the never-implemented `plans/pi-caching` research writeup so `plans/` only holds canonical ledgers.

---

## Motivation / context

- Plans: `plans/v0.0.1/`
- Issues: see **Related issues**

---

## Changes

### Name alignment

- `README.md`, `AGENTS.md`, `TASKS.md`, `docs/*`, `plans/v0.0.1/*`, `skills/tmux-debug/SKILL.md`: `lazyKoder` -> `lazykoder`
- `main.go`: package comment and stderr prefixes
- `internal/ui/chat/*`: header brand, prompt placeholder, test expectations
- `internal/modelscache/catalog.go`: User-Agent header
- `internal/ui/confirm/confirm.go`: aliased bubbletea import for consistency
- `.gitignore`, `.golangci.yml`: comment alignment

### Plan cleanup

- Deleted `plans/pi-caching/pi-caching.md` (research note on prompt-cache hit rates, never implemented)

---

## Impact

| Area | Impact |
|------|--------|
| **Performance** | None |
| **Memory** | None |
| **Behavior / correctness** | None (name-only string changes; no logic touched) |
| **API / CLI** | None |
| **Dependencies** | None |
| **Binary size / build time** | None |

---

## Breaking changes / migration

| Item | Migration |
|------|-----------|
| None | - |

---

## Test plan

- [x] `make test` (`go test ./...`) - all packages pass
- [x] `go vet ./...` - clean
- [x] `go build ./...` - clean

### Commands

```sh
make test
make vet
```

---

## Screenshots / sample output

```
ok  	github.com/chinmay-sawant/lazykoder/internal/ui/chat	4.484s
(19/19 packages ok, no failures)
```

---

## Related issues

- Relates to #3 (PR `chore/align-name-lazykoder` merged the repo/URL rename; this sweep finishes the product-name alignment)

---

## PR metadata checklist (author)

- [x] Self-assigned (`--assignee @me`)
- [x] Labels applied
- [ ] Related issues filled with real ticket IDs
- [ ] Filled body committed under `plans/PR/pr-<slug>.md` when process-gated

---

## Follow-ups (out of scope)

-

---

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] No unrelated changes in diff
- [ ] Public API / CLI changes documented
- [ ] New rules have fixture coverage when applicable
- [ ] PR has assignee and labels
- [ ] Related issues use correct Closes/Relates keywords
- [ ] No secrets or generated artifacts committed
