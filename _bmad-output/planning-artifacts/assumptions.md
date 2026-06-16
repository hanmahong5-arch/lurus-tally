# Tally V1.5 — Falsifiable 假设台账

> Schema 来源：`./roadmap-v1.5.md` H1/H2/H3 段。
> 写入：`bin/assumption-snapshot.sh`（S0-C2，每天 08:00 在 governance 机 cron）每日 scrape Prom + manual fields → 更新 `current_value` + `last_evidence_url`。
> 评分：每 4 周 founder 周一会议 review 一次；连 2 周 red → 触发 KS 飞书告警 + pivot 会议（按 `./roadmap-v1.5.md` Kill Switch wiring 段）。
> 验真窗口：90 天首客 cohort（Sprint 4 起，Sprint 12 末最终评分）。

---

## H1 — 付费意愿（跨境老板 ¥3000/年）

```yaml
id: H1
hypothesis: "跨境老板愿付 ¥3000/年 for 多平台合单 + Kova 周一补货"
owner: TBD (S0-C1 founder 签字)
deadline: 2026-W36 (Sprint 12 end ≈ 2026-08)
falsification_threshold: <3/8 trials convert at >= ¥3000
evidence_source:
  - prometheus: tally_trial_conversion_d90
  - manual: signed_contracts.csv
current_value: pending
status: pending
last_evidence_url: null
last_updated: null
```

**证伪行动**：< 3/8 选 ¥3000+ → 砍包月策略，改订单量阶梯或回退老板个人订阅。

---

## H2 — OCR + 群机器人替代 Excel（retail persona）

```yaml
id: H2
hypothesis: "零售老板娘愿意把 OCR + 微信群机器人作为唯一进销存（取代手抄+Excel）"
owner: TBD (S0-C1 founder 签字)
deadline: 2026-W36
falsification_threshold: <3/5 retail trials completely stop using Excel within 14 days
evidence_source:
  - manual: retail_pilot_log.md (founder weekly 1v1 transcripts)
  - prometheus: tally_ocr_invoice_recorded_total{tenant}
  - prometheus: tally_feishu_dingtalk_confirm_total{tenant}
current_value: pending
status: pending
last_evidence_url: null
last_updated: null
```

**证伪行动**：< 3/5 完全停用 Excel → retail persona 砍"全替代"，可能整体退出 retail，V1.5 聚焦 cross_border。

---

## H3 — ⌘K + AI Drawer 双件套 DAU 渗透

```yaml
id: H3
hypothesis: "⌘K + AI Drawer 双件套让付费客户 90 天 DAU 渗透率 ≥ 60% (Palette) / ≥ 30% (Drawer)"
owner: TBD (S0-C1 founder 签字)
deadline: 2026-W36
falsification_threshold:
  - palette_dau_penetration_d90 < 0.40
  - ai_drawer_dau_penetration_d90 < 0.30
evidence_source:
  - prometheus: tally_palette_invocation_dau / tally_total_dau (Sprint 6 起，bin/assumption-snapshot.sh 日算)
  - prometheus: tally_ai_drawer_open_dau / tally_total_dau
  - event source: web/lib/telemetry.ts palette_invocation / ai_drawer_open (S0-Q3 落地)
current_value: pending
status: pending
last_evidence_url: null
last_updated: null
```

**证伪行动**：< 40%（⌘K）或 < 30%（Drawer）→ "Linear 级体验"在 SMB 不成立，重评估差异化路径（可能砍 F3.3 NLQ 投入，加重 F4 信任打钉 + 自动化）。

---

## 评分约定

| Status | 含义 |
|---|---|
| `pending` | 数据未到 / 尚未达 deadline |
| `truthy` | 满足非证伪阈值（即未达到 falsification_threshold） |
| `falsified` | 命中 falsification_threshold → 触发 pivot 会议 |
| `inconclusive` | 数据噪声大 / sample n < 阈值 → 延期 1 sprint，仍 inconclusive → 视同 falsified |

---

## 状态历史

> `bin/assumption-snapshot.sh` 每日 append 一行；超过 90 天 trim。

| 日期 | H1 status | H1 current_value | H2 status | H2 current_value | H3 status | H3 current_value | KS1 status | KS1 value | KS2 status | KS2 value |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-05-18 | pending | — | pending | — | pending | — |
| 2026-05-20 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-21 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-22 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-23 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-24 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-25 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-26 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-27 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-28 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-29 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-30 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-05-31 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-01 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-02 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-03 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-04 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-05 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-06 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-07 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-08 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-09 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-10 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-11 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-12 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-13 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-14 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-15 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
| 2026-06-16 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
