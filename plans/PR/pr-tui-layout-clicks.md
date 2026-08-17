## Summary

Ship the v0.0.5 TUI layout ledger and the click/layout fixes found in live tmux walks: settings and help teach the product, `@` can still reach files, and mouse hits land on the painted chips, picker rows, and tool chevrons.

## Motivation / context

- Plans: `plans/v0.0.5/README.md` (ratings, phases, closure gates)
- Live review: tmux `lazykoder-ui-qa` at 167x48 and 80x24 on 2026-08-17

## Changes

### Layout and settings

- Opaque confirm / ask / `@` cards
- `@` picker sections plus a scroll window so files stay reachable with many sub-agents
- Settings card: sections, timeout / role / confirm / queue / explore model, mouse on every row
- `/help` lists `/settings`, `/new`, `/continue`, `/refresh`
- Grouped slash palette, collapsed todos, model and variant footer chips
- Resume empty frame stays 80% tall

### Click and chrome fixes (this push)

| Symptom | Cause | Fix |
| --- | --- | --- |
| Model / variant `▾` only opened if you clicked below or off to the side | Chip boxes overlapped; variant was tested first | Paint-scan each label; nearer chip wins |
| Clicking a variant row did nothing | List Y assumed the drawer sat under the transcript | Map rows from the painted `reasoning ·` / `models ·` header |
| `▸` thinking / tool headers needed a click one row low | `transcriptTop` added a phantom `+ 1` for a newline that does not create a row | Drop that `+ 1` |
| Enter left `│` / ticks / scrollbar junk on the left | User-nav overlay invented extra rows into the composer | Only paint ticks on rows that already exist |
| Todo expand moved the right-side dots | Rail respread on the shorter transcript | Stable span as if todos stayed one line |

## Ratings (from `plans/v0.0.5/README.md`)

| Lens | Before | After |
| --- | --- | --- |
| Overall product | 6.0 / 10 | 8.0 / 10 |
| TUI / layout | 5.5 / 10 | 8.0 / 10 |
| Settings completeness | 4.0 / 10 | 8.5 / 10 |
| Discoverability | 5.0 / 10 | 7.5 / 10 |
| Compact 80x24 | 3.5 / 10 | 7.0 / 10 |

## Impact

| Area | Impact |
| --- | --- |
| **Performance** | None |
| **Memory** | None |
| **Behavior / correctness** | Mouse hit-testing and composer chrome |
| **API / CLI** | None |
| **Dependencies** | None |
| **Binary size / build time** | None |

## Breaking changes / migration

| Item | Migration |
| --- | --- |
| None | - |

## Test plan

- [x] `go test ./internal/ui/chat -count=1` exit 0
- [x] `go vet ./internal/ui/chat` exit 0
- [x] `go test ./... -count=1` exit 0 (v0.0.5 layout commit)
- [ ] `make run` in a real terminal: click model `▾`, variant `▾`, a variant row, a tool `▸` with todos open, then send a turn

### Commands

```sh
go test ./internal/ui/chat -count=1
go vet ./internal/ui/chat
make run
```

## Related issues

- Relates to `plans/v0.0.5/` (layout ledger)
- Relates to `plans/v0.0.1/findings/` (earlier chrome, already shipped)

## Follow-ups (out of scope)

- `@` still leads with sub-agents; files are below the fold but reachable
- Compact 80x24 footer still truncates the model id

## Reviewer checklist

- [ ] Behavior matches summary and test plan
- [ ] No unrelated changes in the latest click-fix commit (`235bae6`)
- [ ] Public API / CLI unchanged
- [ ] PR has assignee and labels
