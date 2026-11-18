# Playbook: adversarial review round (design or code)

Goal: every lane returns `VERDICT fully done`.

## Lanes

| Lane | Reviewer |
|---|---|
| Security / architecture / code | Codex Astra, high |
| Legal / claims / text | Grok 4.7, high |
| Workflow simulation | internal Claude subagent (bookkeeper, Steuerberater, Prüfer personas) |

## Steps

1. Stage the UI sources inside the repo: `.review/` (gitignored). Grok and Codex cannot read the scratchpad.
2. Write `brief.md` with a quoted heredoc (`<<'EOF'`). Include:
   - the paths;
   - the owner's deliberate decisions, listed as "not defects";
   - first line `VERDICT fully done` or `VERDICT changes: <n>`;
   - a word cap.
3. Round 1: launch Codex and Grok via `spawn.sh` as a background Bash call (parallel, `&` plus `wait`). Launch the internal agent with Agent `run_in_background`.
4. Judge every finding: fix it, or reject it with a reason. Record each report in `docs/research/reviews/rK-<lane>.md`.
5. Next round:
   - make a new run dir and **copy `session-id` + `session.json` from the previous run dir** first (see known.md);
   - run `--resume-id <id>` with the same model and effort;
   - the brief asks the reviewer to verify the previous items and report only NEW defects.
6. Re-brief the internal agent with SendMessage (it resumes with its context).
7. Stop when all lanes report `fully done`. Then:
   - update `known.md` and `followup.md`;
   - delete `.review/`.

## Session IDs (this repo, design review 2026-11-18)

- Codex: `01a1ee20-df47-7b41-a63c-ebdc1fb3be55`
- Grok: `6451ef3e-9ef7-4081-9821-06c443f5e5f4`

Valid only while the CLIs keep their session history. For code reviews, start new sessions.
