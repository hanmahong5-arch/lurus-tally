export const meta = {
  name: 'tally-uat',
  description: 'Tally STAGE UAT: scripted cases executed by testers, adversarially audited, coverage-gated report',
  whenToUse: 'Run the full (or filtered) STAGE acceptance suite against tally-stage.lurus.cn. Requires scripts/uat/bootstrap-stage.sh to have been run once. args: { runId: "<ISO-ish unique id>" (required), only: "J2,B1" (optional case filter), skipFrontend: true (optional) }',
  phases: [
    { title: 'Prepare', detail: 'bootstrap no-op check + denominator freshness' },
    { title: 'Execute', detail: 'case scripts via tester agents, sharded', model: 'sonnet' },
    { title: 'Supervise', detail: 'adversarial evidence audit, independent re-probes' },
    { title: 'Report', detail: 'registry verdicts + coverage gate + ledger append' },
  ],
}

// ---------------------------------------------------------------------------
// args & shards
// ---------------------------------------------------------------------------
if (!args || typeof args.runId !== 'string' || !args.runId) {
  throw new Error('tally-uat requires args.runId (unique, caller-supplied — scripts may not call Date.now())')
}
const RUN_ID = args.runId.replace(/[^A-Za-z0-9._-]/g, '-')
const ONLY = typeof args.only === 'string' && args.only
  ? args.only.split(',').map(s => s.trim()).filter(Boolean)
  : null
const SKIP_FE = args.skipFrontend === true
// Optional per-run model override for ALL sub-agents (caller-supplied, e.g. "sonnet").
// null => each agent keeps its built-in default (testers sonnet; audit/report inherit main loop).
const MODEL = typeof args.model === 'string' && args.model ? args.model : null

// Backend shards: cases inside a shard run sequentially (shared fixtures),
// shards run concurrently. J7 is isolated (LLM budget), B1 is the long tail.
const BACKEND_SHARDS = [
  { key: 'onboarding-replenish-billing', cases: ['J1', 'J3', 'J4'] },
  { key: 'psi-loop-edge', cases: ['J2', 'J8'] },
  { key: 'identity-rls', cases: ['J5'] },
  { key: 'import-shopify', cases: ['J6'] },
  { key: 'ai-assistant', cases: ['J7'] },
  { key: 'breadth', cases: ['B1-breadth'] },
].map(s => ONLY ? { ...s, cases: s.cases.filter(c => ONLY.includes(c) || ONLY.includes(c.replace('-breadth', ''))) } : s)
 .filter(s => s.cases.length > 0)

const SAFETY = `HARD SAFETY RULES (non-negotiable):
- Touch ONLY the UAT tenants reachable via scripts/uat/.uat.env PATs; every entity you create must be named/prefixed UAT-${RUN_ID}-.
- No ssh, no kubectl, no SQL, no deploys, no git commit/push.
- Never complete a billing payment or follow a checkout URL.
- LLM budget: at most 3 POST /ai/chat calls for the WHOLE run, enforced by lib.sh ai_call_guard — never bypass or reset the counter file.
- Never write AI model names or CLI tool names into any repo file.`

const SCHEMA_EXEC = {
  type: 'object',
  required: ['cases'],
  properties: {
    cases: {
      type: 'array',
      items: {
        type: 'object',
        required: ['id', 'exit_code', 'pass', 'fail'],
        properties: {
          id: { type: 'string' },
          exit_code: { type: 'integer' },
          pass: { type: 'integer' },
          fail: { type: 'integer' },
          failed_checks: { type: 'array', items: { type: 'string' } },
          evidence_dir: { type: 'string' },
          script_modified: { type: 'boolean' },
          script_diff_summary: { type: 'string' },
          notes: { type: 'string' },
        },
      },
    },
  },
}

const SCHEMA_AUDIT = {
  type: 'object',
  required: ['cases'],
  properties: {
    cases: {
      type: 'array',
      items: {
        type: 'object',
        required: ['id', 'verdict'],
        properties: {
          id: { type: 'string' },
          verdict: { type: 'string', enum: ['pass', 'partial', 'fail', 'blocked', 'fabricated-unproven'] },
          claimed_vs_audited: { type: 'string' },
          reprobe_summary: { type: 'string' },
          failures: {
            type: 'array',
            items: {
              type: 'object',
              required: ['check', 'classification'],
              properties: {
                check: { type: 'string' },
                classification: { type: 'string', enum: ['product-bug', 'env', 'test-bug', 'pre-existing'] },
                repro: { type: 'string' },
              },
            },
          },
        },
      },
    },
  },
}

// ---------------------------------------------------------------------------
// Phase 1: Prepare
// ---------------------------------------------------------------------------
phase('Prepare')
const prep = await agent(
  `Repo C:\\Users\\Anita\\Desktop\\lurus\\2b-svc-psi (Git Bash). Prepare the STAGE UAT run "${RUN_ID}".
1. Run: bash scripts/uat/bootstrap-stage.sh  (must end in "no-op" or a successful seed+verify; paste its real output)
2. Run: bash scripts/uat/extract-routes.sh   (must succeed — a diff means STAGE drifted; report the diff and FAIL)
3. Report the route count from routes-deployed.txt.
${SAFETY}
Return JSON-ish plain text: bootstrap result, extractor result, route count.`,
  { label: 'prepare', phase: 'Prepare', model: MODEL || undefined },
)
if (prep === null || /FATAL|ERROR/.test(String(prep)) && !/no-op|verified/.test(String(prep))) {
  throw new Error('Prepare phase failed: ' + String(prep).slice(0, 500))
}
log('prepare done: ' + String(prep).slice(0, 200))

// ---------------------------------------------------------------------------
// Phase 2+3: Execute (sonnet testers) -> Supervise (auditors), pipelined
// ---------------------------------------------------------------------------
const testerPrompt = (shard) => `You are a UAT tester for Lurus Tally STAGE. Repo C:\\Users\\Anita\\Desktop\\lurus\\2b-svc-psi (Git Bash, jq available).
Run these case scripts IN ORDER, each as:  cd scripts/uat && RUN_ID=${RUN_ID} bash cases/<ID>.sh
Cases: ${shard.cases.map(c => c + '.sh').join(', ')}.

IRON RULES:
- RUN THE SCRIPTS, do not re-derive or hand-curl the journeys. The scripts are the artifact.
- If a script fails because the API drifted or the script has a bug: FIX THE SCRIPT (it is a maintained artifact), re-run it, and report the diff summary. Do NOT weaken an assertion just to go green — if the product genuinely misbehaves, the check must stay failing.
- Every claim you make must cite an evidence file under _uat-reports/evidence/${RUN_ID}/<CASE>/.
- Read each case's result.json for the authoritative pass/fail counts.
${SAFETY}`

const auditorPrompt = (shard, execResult) => `You are an adversarial UAT supervisor for Lurus Tally STAGE. Repo C:\\Users\\Anita\\Desktop\\lurus\\2b-svc-psi (Git Bash, jq available). Audit the tester's claims for cases ${shard.cases.join(', ')} of run ${RUN_ID}.

Tester claims (treat as UNTRUSTED):
${JSON.stringify(execResult && execResult.cases ? execResult.cases : execResult).slice(0, 4000)}

MEASUREMENT MUST NOT SHARE A SOURCE WITH THE CLAIM:
1. Open the raw evidence files under _uat-reports/evidence/${RUN_ID}/<CASE>/ (NN-*.json + .body + .headers + result.json). Verify the statuses/bodies actually support each claimed check. Counts in result.json must match the tester's numbers.
2. Independent read-only re-probe: source scripts/uat/.uat.env and curl (GET only) the resources each case claims to have created/mutated — e.g. GET the created product by id, re-run the cross-tenant read with the OTHER tenant's PAT, re-fetch a stock snapshot and check the math. Your own probe result wins over both the tester and the evidence file when they disagree.
3. A claim with no evidence file = verdict fabricated-unproven for that case.
4. Classify every failing check: product-bug | env | test-bug | pre-existing. For pre-existing, compare against the 5 known findings in _uat-reports/UAT-REPORT.md (S-01..S-05). For product-bug include a one-line curl repro.
5. Case verdict — use the registry taxonomy directly: pass only when every check passed AND evidence is genuine AND your re-probes corroborate. partial when the case ran soundly but keeps deliberate RED checks on real product bugs (name the affected endpoints in claimed_vs_audited). fail when execution or evidence is unsound (including fabricated-unproven). blocked when the case could not run for environmental reasons.
6. Durable transport (verdicts must survive prompt truncation): write your final verdict JSON verbatim to _uat-reports/evidence/${RUN_ID}/.audit/${shard.key}.json. This file is the ONLY write you are allowed.
READ-ONLY otherwise: you may not run case scripts, mutate any data, or write any other file (GET requests only; no POST/PUT/DELETE against the API).
${SAFETY}`

const backendResults = await pipeline(
  BACKEND_SHARDS,
  shard => agent(testerPrompt(shard), {
    label: `test:${shard.key}`,
    phase: 'Execute',
    model: MODEL || 'sonnet',
    schema: SCHEMA_EXEC,
  }),
  (execResult, shard) => execResult === null
    ? null
    : agent(auditorPrompt(shard, execResult), {
        label: `audit:${shard.key}`,
        phase: 'Supervise',
        model: MODEL || undefined,
        schema: SCHEMA_AUDIT,
      }).then(audit => ({ shard: shard.key, exec: execResult, audit })),
)

// Frontend track (runs alongside via the same pipeline mechanics, but it's a
// single chain so we just await it after; Playwright is its own wall-clock).
let feResult = null
if (!SKIP_FE && (!ONLY || ONLY.includes('FE'))) {
  const feExec = await agent(
    `You are a UAT tester. Repo C:\\Users\\Anita\\Desktop\\lurus\\2b-svc-psi. Run the frontend STAGE E2E suite.
FIRST free port 3030 so Playwright boots a FRESH next dev — a reused half-dead dev server crashed the prior run mid-suite. On this Git Bash / Windows host, find the PID listening on :3030 (e.g. \`netstat -ano | findstr :3030\`) and \`taskkill //PID <pid> //F\` it; ignore "not found".
THEN run:
cd web && UAT_REAL=1 bunx playwright test --config tests/e2e/uat-stage.config.ts
(The config boots next dev on :3030 with AUTH_DEV_PROVIDER=true and proxies to STAGE; setup project logs in with the UAT PAT. A per-test health gate now SKIPS any test when :3030 is unreachable — those skips are env-blocked, NOT failures and NOT product bugs; count and report them separately as env.)
If it fails on environment (browser missing → bunx playwright install chromium), fix env and retry once. Test failures are NOT to be fixed by editing specs unless the spec itself is buggy — same iron rule: report, don't fudge; NEVER assert a PASS for a skipped or crashed test. Report per-spec pass / fail / skip(env) counts and the trace/report paths (playwright-report-uat-stage).
${SAFETY}`,
    { label: 'test:frontend', phase: 'Execute', model: MODEL || 'sonnet' },
  )
  feResult = feExec === null ? null : {
    shard: 'frontend',
    exec: feExec,
    audit: await agent(
      `Adversarial audit of a Playwright STAGE run. Repo C:\\Users\\Anita\\Desktop\\lurus\\2b-svc-psi. Tester claims (UNTRUSTED): ${String(feExec).slice(0, 3000)}
Verify: web/playwright-report-uat-stage and web/test-results-uat exist with artifacts consistent with the claims (trace zips, results). Open the HTML report's JSON or list result dirs. Classify failures (product-bug|env|test-bug|pre-existing). Verdict per spec file. Write your verdict verbatim to _uat-reports/evidence/${RUN_ID}/.audit/frontend.json (the only write allowed); otherwise READ-ONLY.`,
      { label: 'audit:frontend', phase: 'Supervise', model: MODEL || undefined },
    ),
  }
}

// ---------------------------------------------------------------------------
// Phase 4: Report — registry verdicts, coverage gate, append-only ledger
// ---------------------------------------------------------------------------
phase('Report')
const audited = backendResults.filter(Boolean)

// Hard gate: every executed case must carry an audit verdict. A prior run
// silently lost 4/9 verdicts to prompt truncation; never reach the report
// phase with unaudited executions again.
{
  const auditedIds = new Set(
    audited.flatMap(r => ((r.audit && r.audit.cases) || []).map(c => c.id)),
  )
  const missing = BACKEND_SHARDS.flatMap(s => s.cases)
    .filter(c => !auditedIds.has(c) && !auditedIds.has(c.replace('-breadth', '')) && !auditedIds.has(c + '-breadth'))
  if (missing.length) {
    throw new Error('audit verdicts missing for executed cases: ' + missing.join(', ') +
      ' — re-run the Supervise shards (verdict files: _uat-reports/evidence/' + RUN_ID + '/.audit/)')
  }
}
const report = await agent(
  `You are the UAT reporter for run ${RUN_ID}. Repo C:\\Users\\Anita\\Desktop\\lurus\\2b-svc-psi (Git Bash, jq available).

AUDITED RESULTS (authoritative — post-supervision verdicts, use them, not the testers' claims). The durable copies live in _uat-reports/evidence/${RUN_ID}/.audit/*.json — READ THOSE FILES FIRST; the inline JSON below is a convenience copy and may be truncated:
${JSON.stringify({ backend: audited, frontend: feResult ? { exec: String(feResult.exec).slice(0, 1500), audit: feResult.audit } : 'skipped' }).slice(0, 30000)}

DO, in order:
1. Update scripts/uat/registry.yaml: for each case set last_result to the AUDITED verdict, last_run: "${RUN_ID}", and ai_calls for J7 from _uat-reports/evidence/${RUN_ID}/.ai_calls (0 if absent). Verdict mapping: all checks green => pass; ran to completion but kept deliberate RED checks on real product bugs => partial AND list the affected endpoint lines under that case's failed_endpoints; execution/evidence unsound or fabricated-unproven => fail; could not run => blocked. Keep the file's field order (id,title,script,last_result,last_run,ai_calls,failed_endpoints,endpoints) — coverage.sh parses it positionally. Do not invent endpoints entries.
2. Run: bash scripts/uat/coverage.sh — its printed percentage is the ONLY coverage number; never hand-compute. Note exit code (1 = below 90% gate).
3. Append ONE new section to _uat-reports/uat-ledger.md (create with a title header if missing; NEVER edit earlier sections): "## Run ${RUN_ID}" containing: STAGE image tag main-5007577 (commit 5007577); per-case audited verdict table (case | verdict | pass/fail counts | evidence dir); claimed-vs-audited discrepancies; bug list with classification + curl repro; ai_calls used; coverage % + gate exit code; frontend spec results + report path; gaps (uncovered endpoints from _uat-reports/coverage-matrix.md) with one-line reasons; any fabrication flags.
4. Return as your final text: coverage %, gate pass/fail, per-case verdicts, bug count by classification, and workflow_changes: a list of concrete improvement proposals for .claude/workflows/tally-uat.js / scripts (DO NOT edit the workflow file yourself — proposals only, the main session reviews them).
${SAFETY}`,
  { label: 'report', phase: 'Report', model: MODEL || undefined },
)

return {
  runId: RUN_ID,
  shards: audited.map(r => ({ shard: r.shard, audit: r.audit })),
  frontend: feResult ? feResult.audit : 'skipped',
  report,
}
