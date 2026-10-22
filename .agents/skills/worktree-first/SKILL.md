---
name: worktree-first
description: Use a separate worktree for substantial work; keep tiny, explicit current-branch edits lightweight and safe.
---

# Worktree-First Protocol

## When This Applies

Any time you're about to decide where a change should happen.

Use a separate worktree for substantial work: new features, risky fixes,
logic changes, multi-file edits, or anything likely to take more than a
quick pass.

For tiny, explicit edits — a typo, label rename, comment fix, or one-line
copy tweak — it is okay to stay on the current branch when that is clearly
what the user wants. If it is not clear, ask: "Do you want this done here
or in a separate workspace?"

## The Rule

**Match the workspace to the risk.** The root worktree (the top-level
repository directory) is a shared physical checkout — multiple concurrent
sessions read from it and write to it. For substantial work, create an
isolated worktree. For tiny current-branch work, do not scoop up unrelated
local changes: checkpoint only the explicit paths you touched.

## Steps

1. **Use the `git_worktree` tool.** Not raw `git checkout -b`, not
   `git worktree add` from the shell — the tool wraps both and adds the
   safety checks (clean baseline, target dir git-ignored, trunk
   resolution). Pass:
   - `path`: `../<repo-name>-<feature-slug>` — a **sibling**
     directory to the repo root, not a subdirectory of it. Sibling paths
     avoid gitlink-submodule confusion (git treats a linked-worktree
     directory *inside* the tracked tree as a submodule pointer, which
     creates persistent `M` entries in `git status` on
     the root as the worktree's HEAD advances). Example: for a repo at
     `/git_repos/foo/Cercano`, a good worktree path is
     `/git_repos/foo/Cercano-runtime-dashboard`.
   - `branch`: `feat/<feature-slug>` or `fix/<feature-slug>`.
   - `trunk`: the target trunk (usually `main`).

2. **Do all work inside the worktree.** Every `cd`, test run, edit, and
   commit lives in the isolated directory. The root stays on trunk,
   untouched.

3. **Do not `git checkout <branch>` in the root worktree** while your
   feature is active. If you need to look at trunk state, do it in the
   root (which is already on trunk). If you need to look at your branch,
   do it in the worktree.

## After Successful Landing onto Main/Trunk

After an authorized land succeeds, remove the landed feature's linked worktree
by default unless the user explicitly asked to retain it. This is an agent
follow-through step, not automatic cleanup inside git_land. A checkpoint,
failed land, paused conflict resolution, or review/test failure is not a land.

1. Identify the exact feature worktree and target trunk with
   `git worktree list --porcelain`. Never remove the primary/root worktree,
   the current shared trunk checkout, any worktree holding main/trunk, or
   unrelated worktrees. If identity is uncertain, retain it and report why.
2. Verify the landed commits are contained in trunk: capture the post-reconcile
   feature tip (after any rebase) and require
   `git merge-base --is-ancestor <landed-tip> <trunk>` to succeed. Also require
   the candidate worktree's current HEAD to be contained in trunk; do not
   remove a worktree with newer unlanded commits. A successful tool response
   alone is not this verification.
3. Verify the worktree is clean with
   `git -C <feature-path> status --porcelain --untracked-files=all` (empty
   output required). Check for ignored files worth preserving as well; do
   not discard them. Confirm no active session/process is using the worktree
   and it is not locked. If dirty, in use, locked, or checks fail or are
   uncertain, leave it intact and report the remaining path and reason.
4. Move your own command/session working directory outside the candidate,
   to a retained checkout. Recheck identity, containment, and cleanliness
   immediately before running `git -C <retained-checkout> worktree remove <feature-path>`.
   Never force removal: no --force, recursive directory deletion, reset,
   clean, or automatic stash to bypass a refusal. If removal fails, preserve
   the worktree and report the remaining path and reason rather than discard work.
5. Report the landed branch/SHA and whether its worktree was removed or retained
   (with the remaining path and reason). Do not delete the feature branch
   implicitly; branch deletion requires separate explicit authorization.
   Never push as part of this cleanup.

## Why This Exists

The root worktree is the default directory other sessions operate in
when they touch the repo. If your feature branch is checked out there:

- Other agents committing "quick fixes" for unrelated tracks land on
  your branch instead of trunk.
- Any nested worktree left inside the tracked tree gets recorded as a
  submodule pointer whose HEAD keeps drifting — showing up as a
  persistent conflict every time you rebase. The sibling convention
  above sidesteps this entirely.
- Rebasing back onto trunk multiplies conflicts by every stowaway
  commit — each one carries its own worktree-pointer noise that has to
  be reconciled per commit.
- Rolling back is dangerous — other sessions may still be reading the
  checked-out working tree.

A worktree is one tool call to create with `git_worktree`. It isolates
all of this at zero ongoing cost. Skipping the worktree to save one step
now trades minutes for hours of rebase conflict resolution later.

## What This Prevents

- Concurrent-modification hazards between agent sessions sharing the root
- Submodule-pointer conflicts on nested-worktree paths you never touched
  (avoided entirely by the sibling-directory convention above)
- Unrelated commits from other sessions accumulating on your feature
  branch
- "How did I end up with 14 commits I don't recognize?" investigations
  mid-rebase

## Fast Path for Tiny Current-Branch Changes

For genuinely tiny changes — a typo, a label rename, a comment fix, or a
one-line copy tweak — the worktree ceremony is disproportionate. The fast
path is:

- Applies to small edits with no logic changes.
- Stay on the current branch when the user clearly wants a quick edit here.
- If it is not clear, ask whether to work here or in a separate workspace.
- Checkpoint with explicit `paths` and, when the current branch is trunk,
  explicit `allow_trunk`. This stages only the files you name and leaves
  unrelated local work alone.
- Never use raw `git add -A` for current-branch quick edits.

The fast path is deliberately narrow. Anything with a logic change, a new
file, or touching more than a couple of files goes through the full
worktree flow. When in doubt, ask the user which workspace they want.

