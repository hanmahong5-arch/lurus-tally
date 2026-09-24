# Tally STAGE 部署 Runbook (R6)

> 本 runbook 给 `2b-svc-psi`（lurus-tally）首次/常规 STAGE 部署使用。
> 决策依据：[`lurus/doc/decisions/0006-tally-stage-direct-deploy.md`](../../doc/decisions/0006-tally-stage-direct-deploy.md)
> 目标集群：R6 STAGE，Tailscale `100.122.83.20`，公网 `43.226.38.244`，K3s 独立集群（**不进 ArgoCD**）。

---

## 0. 概览

| 项 | 值 |
|---|---|
| 命名空间 | `lurus-tally` |
| 域名 | `tally-stage.lurus.cn` |
| 镜像 | `ghcr.io/hanmahong5-arch/lurus-tally-{backend,web}` |
| 端口 | backend `18200` / web `3000` |
| 入口 | Traefik IngressRoute + `lurus-cn-wildcard-tls` |
| Kustomize 入口 | `2b-svc-psi/deploy/k8s/overlays/stage/` |

**部署不走 ArgoCD**。所有变更通过 `ssh root@100.122.83.20 kubectl ...` 直接生效，git manifest 是真源，但同步是人工动作。

---

## 1. 前提清单（部署前必须完成）

> 缺一项就停下，不要继续。每项都有验证命令。

### 1.1 Zitadel Client 注册

1. 登 https://auth.lurus.cn
2. 进 Projects → 选 Lurus Platform 项目（或 Tally 专用项目，按现有规范）
3. New Application → Application Type **Web** → Authentication Method **PKCE + Client Secret (CONFIDENTIAL)**
4. 填：
   - Application Name: `lurus-tally-stage`
   - Redirect URIs: `https://tally-stage.lurus.cn/api/auth/callback`
   - Post Logout URIs: `https://tally-stage.lurus.cn/`
5. 创建后记下：
   - `ZITADEL_CLIENT_ID`（形如 `999xxxxxxxxxxxxxxxx@lurus`）
   - `ZITADEL_CLIENT_SECRET`（仅展示一次，立即妥善保存）

**验证**：能在 Zitadel 控制台查到 `lurus-tally-stage` client，redirect_uri 准确。

### 1.2 NewAPI Key

复用 lurus 共享 newapi token：从 `重要信息.md`（gitignored）查 `NEWAPI_API_KEY`，或登 https://newapi.lurus.cn 控制台新申请一枚名为 `tally-stage` 的 token。

**验证**：
```bash
curl -fsS https://newapi.lurus.cn/v1/models -H "Authorization: Bearer <NEWAPI_API_KEY>" | head
```
返回 JSON 模型列表即通。

### 1.3 Platform Internal Key

复用 platform 内部统一 key（同 `重要信息.md`），值与其他 platform 服务一致。无新值需申请——单 key 跨服务复用是 platform 设计。

### 1.4 Memorus API Key

从 `重要信息.md` 取 `MEMORUS_API_KEY`。空值 = AI Drawer 无 memory recall（功能降级，不阻塞部署），可后补。

### 1.5 R6 SSH 通

```bash
ssh root@100.122.83.20 "kubectl get nodes -o wide"
```

期望输出：节点 `Ready` 状态，K3s version 显示。失败则 Tailscale 不通或 SSH key 未授权，先解决再回来。

### 1.6 Database / Redis / NATS 连接串

从 `重要信息.md` 或 R6 现有 platform 配置取：
- `DATABASE_DSN`：PostgreSQL，schema `tally`，需具备 RLS 创建权限。形如 `postgres://tally_app:<pwd>@pg.lurus-platform.svc:5432/lurus?sslmode=disable&search_path=tally`
- `REDIS_URL`：DB 5。形如 `redis://redis.lurus-platform.svc:6379/5`
- `NATS_URL`：JetStream 共享。形如 `nats://nats.lurus-platform.svc:4222`

**验证**：在 R6 跑 `kubectl -n lurus-platform get svc | grep -E 'pg|redis|nats'`，确认这些服务存在。如 Tally 是 R6 首个服务，需先 bootstrap PG/Redis/NATS（不在本 runbook 范围）。

---

## 2. Secret 注入（一键覆盖）

> 把 `<填>` 全替换为前提清单收集的真值后整段执行。命令是幂等的——重跑会覆盖旧 secret，pod 下次重启即生效。

```bash
# 2.1 创建/确保 namespace 存在
ssh root@100.122.83.20 "kubectl create namespace lurus-tally --dry-run=client -o yaml | kubectl apply -f -"

# 2.2 注入 secret（覆盖 base/secret.yaml 占位符）
ssh root@100.122.83.20 "kubectl -n lurus-tally create secret generic tally-secrets \
  --from-literal=DATABASE_DSN='<填>' \
  --from-literal=REDIS_URL='<填>' \
  --from-literal=NATS_URL='<填>' \
  --from-literal=PLATFORM_INTERNAL_KEY='<填>' \
  --from-literal=NEWAPI_API_KEY='<填>' \
  --from-literal=MEMORUS_API_KEY='<填>' \
  --from-literal=ZITADEL_CLIENT_ID='<填>' \
  --from-literal=ZITADEL_CLIENT_SECRET='<填>' \
  --from-literal=INTERNAL_API_KEY='<填>' \
  --from-literal=HUB_TOKEN='<填或空字符串>' \
  --dry-run=client -o yaml | kubectl apply -f -"
```

> **HUB_TOKEN** 当前 `internal/pkg/config/config.go` 不读取，可填空字符串（`HUB_TOKEN=''`）。保留 key 是为兼容 base/secret.yaml 列表，不影响 pod 启动。

**验证**：
```bash
ssh root@100.122.83.20 "kubectl -n lurus-tally get secret tally-secrets -o jsonpath='{.data}' | jq 'keys'"
```
返回 11 个 key 名字（base64 编码值不打印，只看 keys 列表）。

---

## 3. 部署

### 3.1 渲染并 apply（Windows / Git Bash）

本机有 kubectl + kustomize：
```bash
cd C:/Users/Anita/Desktop/lurus/2b-svc-psi
kubectl kustomize deploy/k8s/overlays/stage | ssh root@100.122.83.20 "kubectl apply -f -"
```

> 这条管道：本机 kustomize 渲染 → ssh 流转 → R6 kubectl apply。期间 kubeconfig 不需要切换。

### 3.2 备选：本机 kubeconfig 直指 R6

如果你已经把 R6 kubeconfig 配进本机 `~/.kube/config`：
```bash
cd C:/Users/Anita/Desktop/lurus/2b-svc-psi
kubectl --context r6-stage apply -k deploy/k8s/overlays/stage
```

### 3.3 镜像 tag 更新

每次发新镜像后改 `deploy/k8s/overlays/stage/kustomization.yaml` 中 `newTag` 字段，commit + push 到 git，再重跑 3.1。

---

## 4. 验证

```bash
# 4.1 backend rollout
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout status deploy/tally-backend --timeout=180s"

# 4.2 web rollout
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout status deploy/tally-web --timeout=180s"

# 4.3 资源清单
ssh root@100.122.83.20 "kubectl -n lurus-tally get pods,svc,ingressroute -o wide"

# 4.4 backend 健康
curl -fsS https://tally-stage.lurus.cn/internal/v1/tally/health
curl -fsS https://tally-stage.lurus.cn/internal/v1/tally/ready

# 4.5 前端可达
curl -fsSI https://tally-stage.lurus.cn/ | head -20
```

任一步失败：
- pod CrashLoopBackOff → `kubectl -n lurus-tally logs deploy/tally-backend --tail=100`，多半是 secret key 缺失（config.go required 字段）。
- 503 from healthz → DB/Redis/NATS 不通，检查 secret 的连接串。
- 502 from ingress → IngressRoute 未生效或 web pod 未 ready，检查 4.3 输出。

---

## 5. 回滚

```bash
# 5.1 backend
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout undo deploy/tally-backend"

# 5.2 web
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout undo deploy/tally-web"

# 5.3 验证回滚
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout status deploy/tally-backend"
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout status deploy/tally-web"
```

回滚到指定历史版本（看历史）：
```bash
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout history deploy/tally-backend"
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout undo deploy/tally-backend --to-revision=<N>"
```

> 镜像层 rollback 可用上述命令；DB migration rollback 不在本 runbook 范围（迁移目前 forward-only，重大事故需手动 SQL，参见 `_bmad-output/planning-artifacts/architecture.md` migration 章节）。

---

## 6. 升 PROD 触发条件

按 [`lurus.yaml` server-landing-policy](../../lurus.yaml) 与 ADR-0006，**全部满足**才发起 R6 → R1 迁移：

1. **CI 全绿**：`go test -race ./...` + `bun run build` + `golangci-lint run` 在 main 分支连续 3 个版本成功。
2. **真实客户使用**：至少 1 家付费客户在 STAGE 上日常使用 ≥ 14 天，billing 链路（`/api/v1/billing/{overview,subscribe}`）有真实交易记录。
3. **监控数据**：错误率 < 0.5%、p95 latency < 500ms、stockout 0 events。
4. **架构准备**：决定是否把 ArgoCD 改造成 multi-cluster（届时本 ADR-0006 trigger 信号 #2 命中），或仍走人工 apply 上 R1。

升 PROD 时新建 `2b-svc-psi/deploy/PROD_RUNBOOK.md`，并把本 runbook 标记为 STAGE-only。届时 ApplicationSet 是否接管 Tally 由当时多集群成本评估决定。

---

## 附：常用排障命令

```bash
# 查看 pod 日志（最近 200 行）
ssh root@100.122.83.20 "kubectl -n lurus-tally logs deploy/tally-backend --tail=200"

# describe 看 Events
ssh root@100.122.83.20 "kubectl -n lurus-tally describe pod -l app=tally-backend"

# 进 pod shell（debug）
ssh root@100.122.83.20 "kubectl -n lurus-tally exec -it deploy/tally-backend -- sh"

# 实时跟踪日志
ssh root@100.122.83.20 "kubectl -n lurus-tally logs -f deploy/tally-backend"

# 看 manifest diff（部署前预检）
cd C:/Users/Anita/Desktop/lurus/2b-svc-psi
kubectl kustomize deploy/k8s/overlays/stage | ssh root@100.122.83.20 "kubectl diff -f -" || true
```
