---
name: create-pr
description: Create a pull request for this repository using the show-gi PR template (Background / Changes / Impact, written in English). Use whenever the user wants to open a PR here — "PR 만들어줘", "PR 올려줘", "PR 올려", "create PR", "open PR", "submit changes for review". This replaces git-workflow:create-pull-request, which uses the Ecube Labs company template and must not be used in this repo.
---

# Create Pull Request (show-gi)

Open a PR with `gh` using this repo's template. **Do not use `git-workflow:create-pull-request`** — that skill carries the Ecube Labs template with Jira links and a Korean-language body, neither of which applies here.

## Conventions

- **Body is written in English.** The docs and code comments are Korean, the PR body is not.
- **No Jira link.** This is a personal repo with no ticket system.
- **No Self review checklist.** Reviews happen ad hoc, not as a gate.
- **Not a draft.** Solo repo — open it ready.
- Title follows Conventional Commits (`type: description`), under 70 characters.

## Workflow

### 1. Build context

Run these in parallel — the commits and diff tell you what the PR is about, so don't make the user re-explain:

```bash
git branch --show-current
git log main..HEAD --oneline
git diff main..HEAD --stat
```

If the branch is `main`, stop and tell the user to move to a feature branch.
If the branch isn't pushed, push it: `git push -u origin HEAD`.

### 2. Draft the body

```markdown
## Background

<Why this change was needed. One short paragraph.>

## Changes

- <Bulleted list of what changed. Group by area, not by file.>

## Impact

<What this unblocks or improves. One or two sentences.>
```

Keep it tight. Bullets over prose. If a section would be a single obvious line, still include it — the three-part shape is the convention.

If the change touches both `apps/server` and `apps/web`, group the Changes bullets under bold area headings rather than mixing them.

Deadline is 2026-08-15. If a PR knowingly leaves something for a later PR, say so in one line at the end of Impact rather than opening a tracking issue.

### 3. Create

```bash
gh pr create --base main --title "<type>: <description>" --body "$(cat <<'EOF'
<body here>
EOF
)"
```

Show the returned URL and **stop there**. Never merge — the user merges from the GitHub PR page themselves. Don't run `gh pr merge`, and don't ask whether to merge.

## Notes

- Branch naming is `<type>/<kebab-case>` — `docs/`, `feat/`, `fix/`, `chore/`, `refactor/`, `test/`.
- Squash merge is the default for this repo, so the PR title becomes the commit on `main`. Write the title accordingly.
- `main` is protected by a GitHub ruleset: PRs are required, force pushes and deletions are blocked, and the `Server` and `Web` checks must pass before merge. A failing check means the PR is not mergeable — fix it rather than working around it.
