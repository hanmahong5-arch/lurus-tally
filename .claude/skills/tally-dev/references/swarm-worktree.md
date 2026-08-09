> 从 SKILL.md 拆出的深度材料(2026-07-01 渐进披露批次2)。§7 Swarm Worktree 多 agent 并行模式(territory / sequential merge / 冲突 resolution)。

## 7. Swarm Worktree 模式（多 agent 并行）

3+ track 独立可并行时，用 worktree 隔离：

```
launch agent A: isolation: "worktree"  # → .claude/worktrees/agent-XXX/, 自动建分支
launch agent B: isolation: "worktree"  # 另一个 worktree
launch agent C: isolation: "worktree"  # 第三个
```

**显式划分 territory** 避免冲突 — 每个 agent 改的目录不重叠：
- A: 改 `internal/pkg/platformclient/` + 新增 `internal/adapter/platform/` + `internal/adapter/nats/`
- C: 新增 `internal/pkg/memorusclient/` + 改 `internal/app/ai/`
- F: 改 `web/` + `internal/domain/tenant/` + `internal/app/tenant/`

**红线**:
- agent **不 push**（避免冲突），主 session sequential merge
- agent 不互相碰对方 territory（prompt 里写明 "红线: 不要动 X")
- 多 agent 都改的 collision file 必有：`internal/lifecycle/app.go`（每个 agent 都要加 DI）

**Sequential merge** 主流程:
```bash
# 假设 Track C 在主 worktree（已 commit），A / F 在子 worktree
git merge worktree-agent-A --no-ff   # 解冲突，主要在 lifecycle/app.go import 段
go build ./...                        # 验证
git commit                             # finalize merge
git merge worktree-agent-F --no-ff   # 通常无冲突（web 改动）
git push origin main
```

**lifecycle/app.go 冲突 resolution 模式**:
- HEAD（含其他 track）import: `memorusclient` + `platformclient`
- worktree-A 删了 `platformclient` import（改用 `platform.New`）
- 解：保留 `memorusclient`，去掉 `platformclient`（因为 worktree-A 重构后真不用了）。`go build` 验证 unused import。

**踩坑**:
- `git add internal/...` 路径不要含 `.claude/worktrees/...` — 子目录残骸会被 add 进 commit。**精确写文件名**：`git add -- file1 file2 file3`
- worktree 删除：Windows 文件锁可能让 `git worktree remove` 失败，用 `--force --force`。git 元数据清掉就行，物理目录留着不影响下次 push。

