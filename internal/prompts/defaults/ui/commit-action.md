Inspect the current worktree before acting. Run git status --porcelain, git
diff, and git log -5. Summarize the user-scoped changes, write a detailed
conventional commit message, then ask for the existing bash policy
confirmation before running git add -A, git commit, and git push on the current
upstream branch. Do not discard, reset, or clean unrelated changes. If status,
commit, or push fails, explain the exact error and stop.
