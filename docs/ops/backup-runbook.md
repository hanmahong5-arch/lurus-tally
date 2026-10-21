# 备份与恢复手册

适用范围：进销存服务在 R6（STAGE）上的每日备份。备份由集群里的定时任务 `tally-pgbackup` 生成，本手册的两个脚本负责"检查有没有备份"和"证明备份能恢复"。

## 备份是什么

- 每天 02:00（UTC，北京时间 10:00）导出一次 `tally` schema，格式为 pg_dump 自定义格式，文件名为 `YYYY-MM-DD.dump`。
- 存放位置：MinIO 桶 `tally-backup`（mc 别名默认 `lurus-stage-minio`）。凭证来自集群里的 `tally-secrets`，脚本不会读取它们，运维机上的 mc 别名自行配置。
- 小于 1MB 的导出视为失败，不会上传。
- 定时任务的定义在 `deploy/k8s/base/cronjob-pgbackup.yaml`，本手册不改它。

### "14 天保留"的含义

每次备份成功后，任务会删除桶里超过 14 天的文件。因此：

- 桶里始终只有最近约 14 份每日备份，最早只能回到 14 天前的那一天的数据。
- 发现数据问题超过 14 天，就无法再从备份里找回。发现得越晚，能救回的越少。
- 这是整库按日的全量快照，不是实时备份：恢复到的是备份那一刻的状态，之后到现在的录入需要店里补录。
- 保留期内的备份靠"每天备份成功"保证。如果任务连续失败，保留期会被悄悄吃掉，所以必须每天跑新鲜度检查。

## 脚本一：新鲜度检查

`scripts/ops/backup-freshness.sh`，在 R6 上运行，只读。

```bash
bash scripts/ops/backup-freshness.sh
```

输出一行结论，例如：`备份正常：最新为 2026-10-14.dump，3 小时前生成，大小 4MB`。

以下情况非零退出并打印原因：

- 最新备份超过 26 小时（退出码 1）。
- 最新备份小于 1MB（退出码 1）。
- 桶里没有任何 `.dump`（退出码 1），或无法访问桶（退出码 2）。

可调参数（环境变量）：`MC_ALIAS`、`BACKUP_BUCKET`、`MAX_AGE_HOURS`、`MIN_SIZE_BYTES`。

频率：每天一次，放在每日备份任务之后（例如北京时间 11:00），失败要立即处理，不要攒到演练日。

## 每日定时执行与飞书通知

备份失败没人知道就等于没备份。新鲜度检查失败时（过旧、过小、无备份、读取失败），脚本会在设置了 `FEISHU_WEBHOOK_URL` 时向飞书机器人发一条文本消息，内容含环境名（`ENV_NAME`，默认 `stage`）、最新备份时间、阈值和失败原因；未设置时只打印并以非零退出码结束。

先用 `--dry-run` 看将要发送的内容，它只打印 JSON，不发送：

```bash
ENV_NAME=stage bash scripts/ops/backup-freshness.sh --dry-run
```

（仅在检查失败时才有输出 JSON；正常时只打印"备份正常"。）

### 方式一：服务器 cron

在 R6 上用运维账号 `crontab -e`，北京时间每天 11:00 执行（每日备份任务之后）：

```cron
0 11 * * * cd /data/lurus/2b-svc-tally && ENV_NAME=stage FEISHU_WEBHOOK_URL=https://open.feishu.cn/open-apis/bot/v2/hook/<token> bash scripts/ops/backup-freshness.sh >> /data/log/tally-backup-freshness.log 2>&1
```

webhook 地址属于凭证，建议放在仅运维可读的环境文件里再 `source`，不要写进仓库。

### 方式二：K8s CronJob（示例，仅供参考，不在本仓 deploy/ 落地）

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: tally-backup-freshness
spec:
  schedule: "0 3 * * *"   # UTC 03:00 = 北京时间 11:00
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: check
              image: <含 mc、curl、bash 的镜像>
              command: ["bash", "/scripts/backup-freshness.sh"]
              env:
                - name: ENV_NAME
                  value: stage
                - name: FEISHU_WEBHOOK_URL
                  valueFrom:
                    secretKeyRef: { name: tally-feishu-webhook, key: url }
```

Job 失败（非零退出）本身也应由集群监控发现，作为飞书消息发不出去时的兜底。

## 脚本二：恢复演练

`scripts/ops/backup-restore-drill.sh`。做的事：下载备份、在同一个 PG 实例里新建临时库 `tally_drill_<日期>`、恢复 `tally` schema、核对、删除临时库。任何一步失败都会清理临时库和下载文件，并非零退出。写入只发生在 `/data/tmp` 和临时库里，不碰正式库。

R6 上演练最新一份备份：

```bash
export DRILL_DSN='postgres://<有建库权限的账号>:<密码>@<PG 地址>:<端口>/lurus?sslmode=disable'
export SOURCE_DSN="$DRILL_DSN"      # 可选：在对照表里多列一栏"源库当前"
export EXPECT_VERSION=59            # 可选：期望的迁移版本，随发版更新
bash scripts/ops/backup-restore-drill.sh latest
```

也可以指定某一天：`bash scripts/ops/backup-restore-drill.sh 2026-10-10.dump`。

本地演练（不碰 MinIO，直接用本地文件和本地库）：

```bash
pg_dump -d "$DSN" --schema=tally --format=custom --no-owner --file=/tmp/local.dump
DRILL_DSN="$DSN" DRILL_WORKDIR=/tmp/drill bash scripts/ops/backup-restore-drill.sh --local /tmp/local.dump
```

演练会核对并打印对照表：

| 核对项 | 通过条件 |
| --- | --- |
| 迁移版本 | 有值；若设置了 `EXPECT_VERSION` 则必须相等 |
| 迁移未完成标记 | 必须为 f |
| 行数 tenant、product | 必须能读取且不为 0 |
| 行数 bill_head | 必须能读取（新环境可以为 0） |

注意：

- 备份之后正式库还会继续增长，所以"源库当前"一栏行数大于等于恢复结果是正常的；恢复结果比源库多或差得离谱才需要查。
- 账号需要有建库权限，且实例要装有 pgcrypto、vector、pg_trgm 三个扩展（线上它们装在 `tally` schema 里，脚本会在临时库里照样建好）。
- 当天已有同名临时库时脚本拒绝继续，先确认再手动删除。
- 排查问题时可设 `KEEP_DRILL_DB=1` 保留临时库，用完务必手动 `DROP DATABASE`。
- R6 根盘只有 30G，`DRILL_WORKDIR` 默认 `/data/tmp`，不要改到根盘。

频率：**每月一次**完整演练，并记录日期和对照表；发布涉及迁移的大版本之后，再补做一次。

## 恢复单个店铺

场景：某一家店的数据被误删或改乱，其他店正常，不能整库回滚。思路：把备份恢复到临时库，只把这家店（按 `tenant_id`）的数据导出，再导回正式库。

1. 先通知这家店暂停录入，并确认要回到哪一天的备份。
2. 恢复到临时库并保留：

   ```bash
   KEEP_DRILL_DB=1 bash scripts/ops/backup-restore-drill.sh 2026-10-10.dump
   ```

   记下临时库名 `tally_drill_<日期>`。
3. 把这家店在临时库里的数据按表导出（`DRILL_DB_DSN` 指向临时库，`<店铺编号>` 是该店的 tenant_id）：

   ```bash
   # 列出带 tenant_id 的表
   psql -d "$DRILL_DB_DSN" -Atc "select table_name from information_schema.columns      where table_schema='tally' and column_name='tenant_id' order by 1"

   # 逐表导出，文件放在 /data/tmp/shop-restore-<店铺>/restored/ 下
   psql -d "$DRILL_DB_DSN" -c "\copy (select * from tally.<表> where tenant_id='<店铺编号>') to '/data/tmp/shop-restore-<店铺>/restored/<表>.csv' csv header"
   ```

   同样的方式把正式库里这家店现有的数据导到 `current/` 子目录，作为恢复出错时的回退依据。
4. 导入正式库：在维护窗口内，用有足够权限的账号，一个事务里先删这家店在各表中的现有行，再 `\copy` 导入临时库导出的文件。多表之间有外键，事务内先执行 `set session_replication_role = replica;` 跳过外键检查（需要超级用户），提交前核对各表行数。库存数量表和流水必须同批导入，不要只导其中一张。
5. 核对：在应用里打开这家店，抽查商品数、最近的单据、库存是否和备份当天一致。
6. 收尾：删除临时库（`DROP DATABASE tally_drill_<日期>`）和 `/data/tmp/shop-restore-<店铺>/` 里的文件（回退用的那份在确认无误后保留一周再删）。

提醒：恢复会让这家店回到备份时刻，备份之后录入的单据会消失，要让店里补录；恢复前务必让店方确认。

## 出问题时找谁、看什么

- 新鲜度检查失败：先看 `kubectl -n lurus-tally get jobs`、查看最近一次备份任务的日志，确认是导出失败、上传失败还是任务没跑。
- 演练失败：看脚本打印的第一条错误。常见原因是账号没有建库权限、扩展没装、备份文件损坏（此时对更早一天的备份再演练一次，判断问题范围）。
