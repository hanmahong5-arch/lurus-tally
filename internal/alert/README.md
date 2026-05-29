# alert — Kill-Switch Monitoring

## Overview

This package implements the three-signal kill-switch monitoring chain defined
in the Tally V1.5 roadmap.  The CronJob (`cronjob-killswitch.yaml`) runs every
Monday at 09:00 Beijing time, reads the last 14+ days of `assumptions.md`
status history, and fires breach notifications through all configured channels.

---

## Kill-Switch Signals

| ID   | Signal name              | Threshold | Condition   |
|------|--------------------------|-----------|-------------|
| KS1  | `ks1_onboarding_rate`    | 40 %      | value < 0.40 for 14 consecutive days |
| KS2  | `ks2_ai_po_order_rate`   | 20 %      | value < 0.20 for 14 consecutive days |
| KS3  | `ks3_trial_conversion`   | 30 %      | value < 0.30 for 14 consecutive days |

**Source mapping** (from `assumptions.md` status history table):

| Markdown column | Signal |
|-----------------|--------|
| H1 status       | KS1    |
| H2 status       | KS2    |
| H3 status       | KS3    |

Status values are interpreted as:

| Status        | Signal value | Notes                                       |
|---------------|-------------|---------------------------------------------|
| `truthy`      | 1.0 (green) | Hypothesis confirmed for the period         |
| `falsified`   | 0.0 (red)   | Hypothesis explicitly falsified             |
| `pending`     | 0.0 (red)   | Conservative: no evidence = breach          |
| `inconclusive`| 0.0 (red)   | Per protocol: 1-sprint grace then falsified |
| `—` / `n/a`  | 0.0 (red)   | No data = conservative breach               |

---

## Trigger Conditions

A **Breach** is raised when a signal's status remains 0.0 (red) for
**14 or more consecutive calendar days** — two full weeks.

Breaches are cumulative: a single green day resets the streak.

---

## Sender Chain

| Sender         | When active                             | Purpose                    |
|----------------|------------------------------------------|----------------------------|
| `LogSender`    | Always                                   | Permanent audit trail      |
| `FeishuSender` | `FEISHU_WEBHOOK_URL` env non-empty       | Real-time Feishu card alert|
| `EmailSender`  | All six `SMTP_*` env vars non-empty      | Email to founder / ops     |

`MultiSender` wraps all active senders; one failing sender does not block
the others.

---

## Operational Runbook

### 1. Alert fires — initial response (within 2 hours)

1. Confirm the breach is real and not a data pipeline failure:
   - Check `bin/assumption-snapshot.sh` latest run in the governance cron log.
   - Verify the `assumptions.md` history table updated today.
   - If snapshot data is stale → fix the snapshot pipeline first, re-evaluate.
2. Identify the breaching signal(s) from the LogSender JSON (`slog` output in
   the CronJob pod logs):
   ```
   kubectl -n lurus-tally logs job/tally-killswitch-<id>
   ```

### 2. Escalation (within 24 hours)

- Contact **Founder** (see `_bmad-output/planning-artifacts/assumptions.md`
  `owner` field for each hypothesis).
- Share the breach summary: signal name, consecutive days, first red date.
- Schedule a **Pivot Meeting** within 5 business days.

### 3. Pivot Meeting checklist

- [ ] Confirm breach data is accurate (not a monitoring artefact).
- [ ] Review the falsification evidence linked in `last_evidence_url`.
- [ ] Decide: **pivot** (change hypothesis / product direction) or
  **extend** (only if new contradictory evidence gathered within 7 days).
- [ ] Record decision in `doc/decisions/` with rationale.
- [ ] Update `assumptions.md` status and `last_evidence_url`.

### 4. Breach resolution

Once the signal recovers to green for ≥ 3 consecutive days, the streak resets.
The next Monday run will report 0 breaches.  No manual reset needed.

---

## Circuit Breaker — when to silence the alert

Do **not** silence the alert.  The kill-switch is the product's early-warning
system.  If the alert is noisy due to a data issue, fix the data pipeline
(`bin/assumption-snapshot.sh`), not the alert threshold.

To temporarily disable (e.g. during a planned outage):
```bash
kubectl -n lurus-tally patch cronjob tally-killswitch \
  -p '{"spec":{"suspend":true}}'
```
Re-enable after the outage:
```bash
kubectl -n lurus-tally patch cronjob tally-killswitch \
  -p '{"spec":{"suspend":false}}'
```

---

## Local Dry-Run

```bash
# All env empty → falls back to mock data → exits 1 + LogSender output.
go run ./cmd/kill-switch-monitor

# With real assumptions file:
ASSUMPTIONS_FILE=_bmad-output/planning-artifacts/assumptions.md \
  go run ./cmd/kill-switch-monitor
```

---

## Deployment — Substitute Image Tag

`cronjob-killswitch.yaml` uses `main-PLACEHOLDER` as the image tag.  Replace it
before `kubectl apply`:

**Option A — one-shot sed (manual deploy):**
```bash
SHA=$(git rev-parse --short HEAD)
sed -i "s/main-PLACEHOLDER/main-${SHA}/" deploy/k8s/base/cronjob-killswitch.yaml
kubectl apply -f deploy/k8s/base/cronjob-killswitch.yaml
# Restore placeholder after apply so the file stays clean in git:
sed -i "s/main-${SHA}/main-PLACEHOLDER/" deploy/k8s/base/cronjob-killswitch.yaml
```

**Option B — Kustomize overlay (recommended for R6 stage):**
```yaml
# deploy/k8s/overlays/stage/kustomization.yaml
images:
  - name: ghcr.io/hanmahong5-arch/lurus-tally-backend
    newTag: main-<sha7>   # filled by CI or operator
```
Then: `kubectl apply -k deploy/k8s/overlays/stage/`

**Option C — ArgoCD ApplicationSet:** pass `sha7` as a generator param and
render `newTag` in the Application spec.  The base YAML placeholder is never
applied directly in this path.

---

## Follow-up Items (owned by main-line)

1. ~~**Dockerfile** — add a second `go build` stage for `/kill-switch-monitor`.~~ ✅ Done.
2. ~~**Image tag** — replace `main-latest` placeholder.~~ ✅ Done (`main-PLACEHOLDER`).
3. **`tally-killswitch-secrets` Secret** — add Feishu webhook URL and/or
   SMTP credentials via Sealed Secrets or the existing secret injection path.
4. **Volume strategy** — replace the `hostPath /data/repos/lurus-tally` mount
   with a git-sync sidecar or a CI-generated ConfigMap once live customer data
   starts flowing into `assumptions.md`.
5. **ArgoCD ApplicationSet** — if adopting Option C above, add the `sha7`
   generator param to the ApplicationSet template for R6 stage.
