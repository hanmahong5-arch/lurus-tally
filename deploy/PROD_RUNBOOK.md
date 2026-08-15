# Tally PROD Runbook (R1 / 43.226.46.164)

> **状态：预备文档。PROD 至今零集群足迹，是否部署是 owner 发布决策；本文只保证「决定要上时，路径是现成且可验证的」。**
> 所有「当前状态」类描述以 oracle 命令现跑为准，不要相信本文写作时的快照。

## 0. 部署路径决策（2026-08-15，闭合 08-13 普查 P0 #9）

**PROD 首次部署走本 runbook 人工 apply，不注册 ArgoCD ApplicationSet。**

- 依据：R1 ArgoCD 自 2026-07-29 起断 git 连接（改动需手工 `kubectl set image`）。此时把 `lurus-tally` 加回 governance repo `deploy/argocd/appset-services.yaml` 只会得到一个 sync=Unknown 的摆设（同 R6 lucrum-web 的教训：注册了无凭证/无连接的 Application ≠ GitOps）。
- **迁移触发条件**：R1 ArgoCD 恢复 repo 访问后（oracle：R1 上 `kubectl -n argocd get applications -o wide` 各 app 的 SYNC 列不再是 Unknown），取消 `appset-services.yaml` 中 `lurus-tally` element 的注释（见该文件 2026-04-25 注释块），本文 §3 的 apply 节即废止，§1/§2/§4 仍有效。
- STAGE 继续走 `STAGE_RUNBOOK.md`（ADR-0006 未重审）。

## 1. 前置核查清单（每项现跑 oracle，✅ 才许进 §2）

先过 STAGE_RUNBOOK §7 的升级门槛（CI 全绿 / STAGE ≥1 周无 crashloop / 真实客户 / 监控接入），再逐项核：

| # | 前置 | oracle（现跑） | 备注 |
|---|---|---|---|
| 1 | R1 可达且 K3s 正常 | `ssh root@43.226.46.164 "kubectl get nodes"` | R1 Tailscale 已死，走公网 key 直连 |
| 2 | Traefik 在集群内且认 IngressRoute CRD | `ssh root@43.226.46.164 "kubectl get crd ingressroutes.traefik.containo.us ingressroutes.traefik.io 2>/dev/null; kubectl -n kube-system get pods -l app.kubernetes.io/name=traefik"` | base 的边缘=Traefik IngressRoute（与 R6 host-nginx 不同） |
| 3 | 通配证书 Secret 在 `lurus-tally` ns | `ssh root@43.226.46.164 "kubectl -n lurus-tally get secret lurus-cn-wildcard-tls"` | Traefik 要求 TLS secret 与 IngressRoute 同 ns，需从现有 ns 复制；⚠️ R1 证书 2026-09-24 到期，先核有效期再复制 |
| 4 | DNS `tally.lurus.cn` → R1 | `ssh root@43.226.46.164 "dig +short tally.lurus.cn"` 应答 `43.226.46.164` | **必须在 R1 上跑**——本机出站全走代理，本地解析/探测结论不可信 |
| 5 | 🔓 **OPEN：PROD DB 落点未决** | `ssh root@43.226.46.164 "kubectl get pods -A \| grep -iE 'pg\|postgres'; docker ps 2>/dev/null \| grep -iE 'pg\|postgres'"` | R1 无已登记 PG；R2 的 PG 走 Tailscale 而 R1 Tailscale 死。**owner 决策**：R1 自建 PG / 打通 R1→R2 网络 / 其他。DSN 定不下来则一切免谈 |
| 6 | 🔓 **OPEN：备份目标可达性** | 在 R1 探 MinIO endpoint（office-win-1 走 Tailscale，同样受 R1 Tailscale 死影响） | `cronjob-pgbackup` 在 PROD 打不到 MinIO 会 CrashLoop job（不影响主服务）；不通则先 suspend 该 CronJob 并记 owner 队列 |
| 7 | OIDC PROD client 已注册 | 身份提供方控制台（PROD issuer，base configmap 所载）拿到 client_id/secret | Redirect URI `https://tally.lurus.cn/api/auth/callback/oidc`，Post-logout `https://tally.lurus.cn`；owner 控制台操作 |
| 8 | Redis / NATS PROD 侧落点 | 同 #5 一并决策 | STAGE 用 R6 集群内地址，PROD 不可照抄 |

## 2. Secret 注入

**Key 清单不要抄本文**——以两个真源现场推导：
1. STAGE 实况：`ssh -p 12222 root@43.226.45.87 "kubectl -n lurus-tally get secret tally-secrets -o jsonpath='{.data}'" | jq 'keys'`
2. 代码要求：backend `internal/config/config.go` 的 required 校验 + web 的 NextAuth env（`AUTH_SECRET`/`NEXTAUTH_URL`）。

差异处理原则同 STAGE_RUNBOOK §2（含 `HUB_TOKEN`/`INTERNAL_API_KEY` 兼容占位、`OIDC_AUDIENCE` 硬要求）。PROD 特有值：`NEXTAUTH_URL=https://tally.lurus.cn`、OIDC issuer 用 PROD issuer（**不是** test-auth）。

```bash
ssh root@43.226.46.164 "kubectl create namespace lurus-tally --dry-run=client -o yaml | kubectl apply -f -"
# 然后按推导出的 key 清单 create secret generic tally-secrets（写法同 STAGE_RUNBOOK §2）
```

> 🔴 base/secret.yaml 的占位 Secret 已从 PROD render 中剔除（overlay `$patch: delete`）。
> STAGE 从未被占位值覆盖只是**意外**：占位值不是合法 base64，API server 恰好拒收。
> 不要把占位 Secret 加回 PROD render，也不要"修好"它的 base64。

## 3. 部署

```bash
cd C:/Users/Anita/Desktop/lurus/2b-svc-tally

# 渲染自检（不触集群）：无 Secret 对象、镜像 tag 非 placeholder、Host 为 tally.lurus.cn
kubectl kustomize deploy/k8s/overlays/prod | grep -E "kind: Secret|image:|Host\(" 

# 远程 apply
kubectl kustomize deploy/k8s/overlays/prod | ssh root@43.226.46.164 "kubectl apply -f -"
```

- 镜像已钉 `main-530950a`（2026-08-15，CI 绿构建）。**升级 = 改 overlay `newTag` → commit → 重跑 apply**，禁直接 `kubectl set image`（那是漂移源头）。
- 首次 boot 会把全量 migration 跑到当前 head（fresh DB）。确认 §1 #5 的 DSN 指向的是**空 schema 或有意的目标库**再 apply。
- `SEED_NURSERY_DICT` PROD 默认不开（base 即关闭态）；要开走 overlay patch + commit，owner 决策。

## 4. 验证

```bash
ssh root@43.226.46.164 "kubectl -n lurus-tally rollout status deploy/tally-backend --timeout=180s"
ssh root@43.226.46.164 "kubectl -n lurus-tally rollout status deploy/tally-web --timeout=180s"
ssh root@43.226.46.164 "kubectl -n lurus-tally logs deploy/tally-backend --tail=50 | grep -i migration"  # 期望 head=当前最新 dirty=false
curl -fsS https://tally.lurus.cn/internal/v1/tally/ready | jq .   # 200；503 时响应体指明挂的 dep
curl -fsS -o /dev/null -w '%{http_code}\n' https://tally.lurus.cn/  # 200
curl -fsS -o /dev/null -w '%{http_code}\n' https://tally.lurus.cn/api/v1/me  # 无 token 应 401（鉴权在岗）
```

## 5. 回滚

```bash
ssh root@43.226.46.164 "kubectl -n lurus-tally rollout undo deploy/tally-backend && kubectl -n lurus-tally rollout undo deploy/tally-web"
```

> ⚠️ migration 不随 rollout undo 回退。跨 migration 的版本回滚先查对应 down 文件影响面（同 STAGE_RUNBOOK 各 deploy 记录的回滚注意事项）。
