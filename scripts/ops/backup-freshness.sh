#!/usr/bin/env bash
# 备份新鲜度检查：列出 MinIO 里最新一份 tally 备份，过旧或过小则非零退出。
# 在 R6 上运行（需要已配置好的 mc 别名）。只读，不写任何文件。
#
# 环境变量（均有默认值）：
#   MC_ALIAS       mc 别名，默认 lurus-stage-minio
#   BACKUP_BUCKET  备份桶，默认 tally-backup
#   MAX_AGE_HOURS  最新备份允许的最大年龄（小时），默认 26
#   MIN_SIZE_BYTES 最新备份允许的最小字节数，默认 1048576（与每日备份任务的 1MB 门槛一致）
#   MC_BIN         mc 可执行文件，默认 mc
#   ENV_NAME       通知里显示的环境名，默认 stage
#   FEISHU_WEBHOOK_URL  飞书机器人地址；非空时检查失败会 POST 文本消息，为空只打印并非零退出
#
# 参数：--dry-run  只打印将要发送的 JSON，不实际发送。
#
# 退出码：0 正常；1 备份过旧或过小；2 无法读取备份列表。
set -euo pipefail

MC_ALIAS="${MC_ALIAS:-lurus-stage-minio}"
BACKUP_BUCKET="${BACKUP_BUCKET:-tally-backup}"
MAX_AGE_HOURS="${MAX_AGE_HOURS:-26}"
MIN_SIZE_BYTES="${MIN_SIZE_BYTES:-1048576}"
MC_BIN="${MC_BIN:-mc}"
ENV_NAME="${ENV_NAME:-stage}"
FEISHU_WEBHOOK_URL="${FEISHU_WEBHOOK_URL:-}"

DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    *) echo "未知参数：$arg（仅支持 --dry-run）" >&2; exit 2 ;;
  esac
done

LATEST_TIME="未知"

json_escape() { printf '%s' "$1" | tr '\r\n' '  ' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }

# notify <原因>：失败时通知飞书；dry-run 只打印 JSON。
notify() {
  local text json
  text="【Tally 备份告警】环境：${ENV_NAME}；最新备份时间：${LATEST_TIME}；阈值：${MAX_AGE_HOURS} 小时 / ${MIN_SIZE_BYTES} 字节；原因：$1"
  json="{\"msg_type\":\"text\",\"content\":{\"text\":\"$(json_escape "$text")\"}}"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "$json"
  elif [ -n "$FEISHU_WEBHOOK_URL" ]; then
    printf '%s' "$json" | curl -sS -m 15 -X POST -H 'Content-Type: application/json' --data-binary @- "$FEISHU_WEBHOOK_URL" >/dev/null \
      || echo "飞书通知发送失败" >&2
  fi
}

fail() { echo "备份检查失败：$1" >&2; notify "$1"; exit "${2:-1}"; }

command -v "$MC_BIN" >/dev/null 2>&1 || fail "找不到 mc 命令（MC_BIN=$MC_BIN）" 2

listing=$("$MC_BIN" ls --json "${MC_ALIAS}/${BACKUP_BUCKET}/" 2>&1) \
  || fail "无法列出 ${MC_ALIAS}/${BACKUP_BUCKET}：${listing}" 2

# 每行一个 JSON 对象；抽取 key / size / lastModified，按 lastModified 取最新的 .dump。
latest_key=""; latest_size=0; latest_time=""
while IFS= read -r line; do
  key=$(printf '%s' "$line" | sed -n 's/.*"key":"\([^"]*\)".*/\1/p')
  [[ "$key" == *.dump ]] || continue
  size=$(printf '%s' "$line" | sed -n 's/.*"size":\([0-9]*\).*/\1/p')
  mtime=$(printf '%s' "$line" | sed -n 's/.*"lastModified":"\([^"]*\)".*/\1/p')
  [ -n "$mtime" ] || continue
  # ISO 时间字符串可直接按字典序比较
  if [[ -z "$latest_time" || "$mtime" > "$latest_time" ]]; then
    latest_key="$key"; latest_size="${size:-0}"; latest_time="$mtime"
  fi
done <<< "$listing"

[ -n "$latest_key" ] || fail "桶 ${BACKUP_BUCKET} 里没有任何 .dump 备份" 1

LATEST_TIME="$latest_time"
ts=$(date -d "$latest_time" +%s 2>/dev/null) || fail "无法解析备份时间 ${latest_time}" 2
now=$(date +%s)
age_h=$(( (now - ts) / 3600 ))
size_mb=$(( latest_size / 1048576 ))

problems=()
if [ "$age_h" -gt "$MAX_AGE_HOURS" ]; then
  problems+=("最新备份已 ${age_h} 小时（上限 ${MAX_AGE_HOURS} 小时）")
fi
if [ "$latest_size" -lt "$MIN_SIZE_BYTES" ]; then
  problems+=("大小 ${latest_size} 字节低于下限 ${MIN_SIZE_BYTES} 字节")
fi

if [ "${#problems[@]}" -gt 0 ]; then
  msg="最新为 ${latest_key}（${latest_time}，${size_mb}MB）；$(IFS='；'; echo "${problems[*]}")"
  echo "备份异常：${msg}" >&2
  notify "$msg"
  exit 1
fi
echo "备份正常：最新为 ${latest_key}，${age_h} 小时前生成，大小 ${size_mb}MB"
