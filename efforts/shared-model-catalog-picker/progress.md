# Progress

## Baseline and component

- Feature worktree: `/Users/bryancostanich/git_repos/cercano-ai/Cercano-model-catalog-picker`, branch `feat/shared-model-catalog-picker`.
- Delegate attempts encountered output truncation, network failure and context overflow. Main agent continued directly; removed an incomplete delegate probe with nonexistent APIs before running the actual baseline.
- Actual fake-gRPC probe confirmed that form option construction blocks until discovery replies even with `HasKey=false`; a missing key produced a generic `no API key` error; example metadata label was 194 bytes. The baseline test will be replaced by regression assertions when behavior changes.
- Added presentation-only `overlay.Catalog` with typed actions, exact ID/account identity, async entry replacement, search ranking, filters, current/highlight distinction and responsive details.
- Focused component tests passed, including real key messages and all four viewport sizes. The initial bounds test exposed excess wrapping from counting Lip Gloss borders/padding twice; corrected Width to the full viewport allocation and reran successfully.
- This is not yet integrated into cloud settings. No visual/manual terminal verification claimed yet.
