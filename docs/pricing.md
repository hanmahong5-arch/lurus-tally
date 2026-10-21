# Lurus Tally 定价（散文真源）

> 状态：建议，待负责人裁决。数字真源即本文件；`web/lib/billing/plans.ts` 与平台种子须与之同步。

## 套餐

plan code 沿用平台迁移 `025_seed_tally_product`（product_id `lurus-tally`）：`free` / `pro` / `pro_yearly` / `enterprise` / `enterprise_yearly`。

| 档位 | code | 周期 | 价格（建议，待负责人裁决） | 人数 | 仓库 | 商品 |
|---|---|---|---|---|---|---|
| 免费版 | free | 永久 | ¥0 | 1 | 1 | 300 |
| 专业版 | pro | 月付 | ¥58/月 | 5 | 3 | 5000 |
| 专业版（年付） | pro_yearly | 年付 | ¥588/年 | 5 | 3 | 5000 |
| 企业版 | enterprise | 月付 | ¥198/月 | 不限 | 不限 | 不限 |
| 企业版（年付） | enterprise_yearly | 年付 | ¥1980/年 | 不限 | 不限 | 不限 |

## 当前执行状态

- 种子期：`SEED_TRIAL=true`，全部功能免费试用，不开放付费入口。正式收费时改为 false。
- 限额（人数/仓库/商品）当前后端**不执行**，页面不展示数字上限；待 P5 影子模式观察后再执行（错误码规划见治理仓 0038 与 contracts）。
- 平台 025 种子中的旧数字（¥199/¥1990/¥599/¥5990，商品 50/2000）与本文建议值不一致，属待改状态，见下“改价流程”。

## 市场参照

- 小微店铺类进销存：多数 ≤ 600 元/年。
- 主流 SaaS 进销存：288–998 元/年/人。
- 建议价据此定位在小微店可接受区间；最终数字与目标客群由负责人裁决（见治理仓 ADR 0038）。

## 改价流程（三处同步）

1. 平台侧：新增改价迁移（平台 148 号，当前仍是 next available，动手前先在平台 ledger 预留）更新 `identity.product_plans` 的 price_cny 与 features；不要改动 025 已落地的种子。
2. 前端：`web/lib/billing/plans.ts` 的 priceCNY 与 caps，及 `web/app/pricing/page.tsx` 的 metadata 文案。
3. 本文件：更新表格与日期。

三处任一漂移，订阅时可能出现“Plan matching ...”404 或页面价格与账单不一致。

## 限额执行模式

套餐对成员数、商品数、仓库数设有上限，平台权益键为 `max_users` / `max_skus` / `max_warehouses`（兼容旧键 `users` / `skus` / `warehouses`；值缺失、非数字或 -1 一律视为不限）。执行方式由环境变量 `PLAN_LIMITS` 控制，默认 `shadow`：

| 模式 | 行为 |
| --- | --- |
| `off` | 不检查，也不统计用量 |
| `shadow` | 超限时只记一行 Info 日志 `plan_limit_shadow key=… used=… limit=…`，业务照常放行 |
| `enforce` | 超限返回 403 `{code:"plan_limit_reached", message, key, used, limit}`；批量导入商品超限时整批拒绝 |

检查点：团队邀请与加入（成员）、新建商品与商品导入（商品，导入按本批可创建的行数计）、新建仓库。用量口径：成员取本店成员映射行数，商品只数未删除且启用的，仓库只数未删除的。读取套餐或统计用量出错时放行并记 Warn，不会因平台故障锁死店铺。订阅页「用量」表只做展示。

当前为种子期：保持 `shadow`，从日志收集真实用量，用来校准各档上限。正式收费前按顺序做：

1. 平台侧新增改价迁移（平台 148 号，动手前先在 ledger 预留），写入各档的 `max_*` 上限。
2. 把 `PLAN_LIMITS` 翻成 `enforce`（改 `deploy/k8s` 对应环境的 ConfigMap，走 git 源）。
3. 前端 `SEED_TRIAL` 置为 `false`（`web/lib/billing/plans.ts`），套餐页恢复正式计费文案。
