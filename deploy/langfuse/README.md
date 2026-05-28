# Langfuse self-hosted — R6 部署指南

Langfuse 以独立 Docker Compose 栈运行在 R6 (`/data/langfuse/`)，**不接管 tally 数据库**，通过 OTLP HTTP 协议接收来自 tally-backend 的 LLM span。

## 部署步骤

### 1. 在 R6 准备目录与 env 文件

```bash
ssh root@100.122.83.20
mkdir -p /data/langfuse/{pg,redis}

cat > /data/langfuse/.env << 'EOF'
LANGFUSE_PG_PASSWORD=<strong-random>
LANGFUSE_NEXTAUTH_URL=https://langfuse.tally-stage.lurus.cn
LANGFUSE_NEXTAUTH_SECRET=<32-char-random>
LANGFUSE_SALT=<32-char-random>
LANGFUSE_ENCRYPTION_KEY=<32-byte-hex>
EOF
chmod 600 /data/langfuse/.env
```

生成随机值参考：`openssl rand -hex 32`

### 2. 启动栈

```bash
docker compose --env-file /data/langfuse/.env \
  -f /path/to/deploy/langfuse/docker-compose.yml \
  up -d
```

### 3. Healthcheck

```bash
# Postgres
docker exec langfuse-postgres pg_isready -U langfuse -d langfuse

# Web (等 ~60s 初始化完成)
curl -sf http://localhost:3000/api/public/health
# 期望: {"status":"ok"}

# OTel 端点（tally 打到这里）
curl -sf -X POST http://localhost:3000/api/public/otel/v1/traces \
     -H "Authorization: Basic $(echo -n 'pk-lf-xxx:sk-lf-xxx' | base64)" \
     -H "Content-Type: application/json" -d '{}' -o /dev/null -w "%{http_code}"
# 期望: 200 或 400（空 payload）
```

### 4. 创建初始账号

首次访问 `https://langfuse.tally-stage.lurus.cn` → 注册管理员 → 创建 Project → 复制 Public Key / Secret Key。

### 5. 在 tally-backend 注入 env

在 k8s Secret 或 `.env` 中新增三个变量（在 `NEWAPI_API_KEY` 旁边）：

```
LANGFUSE_HOST=https://langfuse.tally-stage.lurus.cn
LANGFUSE_PUBLIC_KEY=pk-lf-xxxxxxxxxxxxxxxx
LANGFUSE_SECRET_KEY=sk-lf-xxxxxxxxxxxxxxxx
```

重启 tally-backend pod：

```bash
kubectl rollout restart deployment/tally-backend -n lurus-tally
```

### 6. 验证 trace 到达

在 Tally AI Drawer 发一条消息，然后登录 Langfuse UI → Traces，应当出现 `llm.chat` span（包含 model / prompt / tokens / latency）。

## 数据安全

- Langfuse 跑在 R6 内网，数据不出境。
- `/data/langfuse/` 目录包含用户数据，备份策略同 tally 数据库（CronJob 每日到对象存储）。

## Nginx / Ingress 配置（参考）

若需通过域名访问，在 R6 Nginx 加：

```nginx
server {
    listen 443 ssl;
    server_name langfuse.tally-stage.lurus.cn;
    location / { proxy_pass http://127.0.0.1:3000; }
}
```

## 后续工作

- **Trace → FE 链接**：AI 响应卡片可携带 `trace_id` 字段，FE 深链到 `{LANGFUSE_HOST}/trace/{trace_id}`。后端需在 `ChatOutput` 中暴露 `TraceID string`，并从 OTel span context 提取。
- **Sampling 降频**：LLM 调用量上升后，将 `AlwaysSample()` 改为 `TraceIDRatioBased(0.2)` 以控制存储。
- **Prompt 脱敏**：生产环境建议在 `truncate` 前移除 PII（邮箱/手机号），可在 `otelSpan.End` 中加 redact 钩子。
- **告警**：Langfuse 支持 Webhook 触发；接入 error rate > 5% 告警。
