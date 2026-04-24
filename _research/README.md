# Research: 智能进销存开源基座选型

3 个并行 Agent 调研三个生态系统，输出统一格式报告供综合决策。

## Files

| File | Source | Owner Agent |
|------|--------|-------------|
| `github.md` | GitHub (国际开源) | Sonnet Agent A |
| `google.md` | Google (市场+趋势) | Sonnet Agent B |
| `gitee.md` | Gitee (国内开源, 重点) | Sonnet Agent C |
| `synthesis.md` | 综合建议 | Opus 主对话生成 |

## Evaluation Criteria

每个候选项目必须按以下维度评估:

1. **License** (MIT/Apache-2.0 优先；AGPL/GPL 谨慎；商用限制必须标注)
2. **Activity** (近 12 月 commit 数；最近一次 release 时间；issue 响应)
3. **Maturity** (stars/forks；生产案例；contributor 数)
4. **Tech Stack** (语言/框架；与 Lurus 栈契合度: Go/React/Postgres 最优)
5. **Feature Completeness** (采购/销售/库存/财务/多仓/多店/多租户/小程序)
6. **Code Quality** (架构清晰度；测试覆盖；文档完备)
7. **Production Readiness** (Docker/K8s 支持；性能数据；监控集成)
8. **Forward-Looking** (AI/LLM 集成空间；可扩展架构；维护者愿景)

## Output Format

每份报告必须包含:
- Executive Summary (3-5 行)
- Top 5-7 候选对比矩阵 (markdown 表格)
- 每个候选的详细分析 (优劣 + 适配性)
- 1-2 个推荐基座 + 理由
- 风险提示 (license/活跃度/技术债)
