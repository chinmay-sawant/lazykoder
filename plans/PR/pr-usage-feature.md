## Summary

Add an OpenCode Go usage surface to lazykoder so users can inspect rolling,
weekly, and monthly plan consumption without leaving the TUI.

## Changes

- Add authenticated `GET /zen/go/v1/usage` support to the OpenCode client.
- Add `/usage` with a centered modal showing percentages, rate-limit status,
  and reset times.
- Display the latest usage values in `/settings` under an OpenCode usage
  section.
- Add refresh handling and focused provider/UI tests.
- Document the command and settings display in `docs/tui.md`.

## Test plan

- `go test ./... -count=1`
- `go vet ./...`

## Notes

The OpenCode API reports a rolling window rather than a fixed hourly window;
the UI labels that window `rolling` to match the API. API keys remain
environment-only and are not written to settings or source files.

## Related issues

- Relates to #
