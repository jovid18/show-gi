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

Run these in parallel — the diff tells you what the PR is about, so don't make the user re-explain:

```bash
git branch --show-current
git diff main...HEAD --stat
git diff main...HEAD --name-status
```

If the branch is `main`, stop and tell the user to move to a feature branch.
If the branch isn't pushed, push it: `git push -u origin HEAD`.

**Describe the diff, not the branch history.** This repo squash-merges, so what lands on `main` is one commit containing the final state. Read the diff; `git log` is for your own orientation only.

Concretely, this means:

- A file created and then deleted on the branch **does not appear in the diff** and must not appear in the body. From `main`'s point of view it never existed.
- Never write "switched from X to Y", "initially did X", "refactored to", or "removed the earlier". If the branch went EC2 → ECS, the PR added ECS. There was no EC2.
- Deletions belong in the body only when the file exists on `main` today.
- Design decisions are worth stating; the order you arrived at them is not. "Deploys run on ECS because the alternative needs bespoke scripts" is useful — "we tried compose first" is noise.

This matters most when **updating** an existing PR, since the body was usually written for an earlier shape of the branch. Rewrite it from the current diff rather than appending to it.

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

If a PR knowingly leaves something for a later PR, say so in one line at the end of Impact rather than opening a tracking issue.

#### Migrations

If the diff adds or edits anything under `apps/server/internal/store/migrations/`, the body **must** name the files that have to be run, as the last line of Impact:

```markdown
**Migration:** `002_anonymous_games.sql`
```

Filenames only — the runbook already says how to run them, and the diff already shows what they contain. The point is that someone re-reading the PR later can see it touched the database without hunting through the diff.

Deploys never run DDL (`deploy/README.md` §4). Schema goes in **before** the merge, applied by hand through a database client, so a merged PR whose migration was skipped leaves the new code running against an old schema.

The `migration` label attaches automatically from the path — don't add it by hand, and don't remove it.

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
