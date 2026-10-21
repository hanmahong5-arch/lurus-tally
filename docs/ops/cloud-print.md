# 云打印（飞鹅云打印机）运维说明

## 作用

收银台、今日结单、销售单详情可把小票发到店里的飞鹅云打印机。服务端调用飞鹅开放平台接口，打印机自己联网取单，不需要店里电脑开着。未配置平台账号时，所有入口自动隐藏，收银台继续用浏览器打印。

## 配置项

| 键 | 位置 | 说明 |
|----|------|------|
| `FEIE_USER` | tally-secrets（密钥） | 飞鹅开放平台账号 |
| `FEIE_UKEY` | tally-secrets（密钥） | 飞鹅开放平台账号的 UKEY |
| `FEIE_API_URL` | configmap（`deploy/k8s/base/configmap.yaml`） | 接口地址，默认 `http://api.feieyun.cn/Api/Open/` |

`FEIE_USER` 或 `FEIE_UKEY` 任一为空，云打印整体关闭；启动日志会有一行 `cloud-print: enabled` 或 `cloud-print: disabled`。

私有化部署（`deploy/customer`）同理：在环境文件里加这两项即可，不配则关闭。

## 出网放行（K8s / 防火墙）

后端 Pod 需要出网访问：

- 域名：`api.feieyun.cn`
- 端口：80（飞鹅开放平台目前是 HTTP 接口）
- 方向：仅出站，无需入站

若集群启用了 NetworkPolicy 或出口白名单，请把上面一条加入 `lurus-tally` 命名空间的 egress 规则（改 overlay 里的网络策略，不要用 kubectl 临时 patch）。排查：在后端 Pod 里 `wget -qO- http://api.feieyun.cn/Api/Open/` 能返回内容即通。

## 接口与权限

| 接口 | 权限 | 说明 |
|------|------|------|
| `POST /api/v1/print/receipts` | `print.receipt` | body `{bill_id, times?}`，返回 `{order_id}` |
| `GET /api/v1/tenant/printer/status` | `print.receipt` | `{configured, online, status, cloud_print_available}` |
| `POST /api/v1/tenant/printer/test` | `print.settings` | 打一张测试页 |
| `PUT /api/v1/tenant/print-settings` | `settings.manage` | 保存打印机编号、密钥、纸宽、自动打印 |

错误码：未配置打印机 409 `printer_not_configured`；打印机离线或状态异常 503 `printer_offline`；平台未开通 501 `cloud_print_unavailable`；绑定失败 422 `printer_bind_failed`。

`print.receipt`：老板、店长、店员、收银；`print.settings`：老板、店长。

## 数据

打印机信息存在店铺设置的 `print.printer` 里（JSON，无需迁移）。密钥库里存全，接口只回显末 4 位。
