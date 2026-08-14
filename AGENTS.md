# AGENTS.md

## Shared project guide

Before doing any work, read [CLAUDE.md](CLAUDE.md) in full. It is the canonical project guide and all of its
architecture, validation, Git, language, documentation, and security rules apply to Codex too.

Keep shared rules in `CLAUDE.md` instead of copying them here. When that guide names Claude as the actor, treat it as
the current coding agent unless the text is specifically distinguishing a Claude product, command, or tool.

## Codex adaptations

- Invoke repository skills with Codex skill syntax: `$create-pr`, `$worktree`, `$playtest`, and `$review-article`.
  References to `/create-pr` or `/worktree` in the shared guide and skills mean the corresponding Codex skill.
- Repository skills are discovered from `.agents/skills`. Their directories are symlinks to the canonical
  `.claude/skills` implementations so Claude and Codex follow one workflow. Edit the canonical files rather than
  creating a divergent Codex copy.
- When a shared skill says to start an `Agent` or use `SendMessage`, use Codex's sub-agent and messaging tools with the
  same sequencing and isolation requirements.
- In worktree handoff instructions, start the coding agent appropriate to the session (`codex` for Codex work,
  `claude` for Claude Code work).
- For this repository's pull requests, always use `$create-pr`. Do not use the Ecube Labs
  `git-workflow:create-pull-request` skill.
