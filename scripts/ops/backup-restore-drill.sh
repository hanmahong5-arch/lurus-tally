#!/usr/bin/env bash
# 备份恢复演练：把一份 tally 备份恢复到同一 PG 实例的临时库，核对后删除临时库。
# 所有写入只发生在工作目录（默认 /data/tmp）和临时库 tally_drill_<日期>。
#
# 用法：
#   backup-restore-drill.sh latest                 # R6：从 MinIO 取最新备份
#   backup-restore-drill.sh 2026-10-14.dump        # R6：从 MinIO 取指定备份
#   backup-restore-drill.sh --local ./some.dump    # 本地：直接用本地文件，不碰 MinIO
#
# 环境变量：
#   DRILL_DSN      PG 管理连接串（必须有建库权限），例如
#                  postgres://user:pass@host:5432/lurus?sslmode=disable
#   SOURCE_DSN     可选；设置后在对照表里多列一栏"源库当前"行数（仅供参考，备份之后源库会继续增长）
#   MC_ALIAS       mc 别名，默认 lurus-stage-minio
#   BACKUP_BUCKET  备份桶，默认 tally-backup
#   DRILL_WORKDIR  下载/临时文件目录，默认 /data/tmp
#   EXPECT_VERSION 可选；期望的迁移版本号，不符则失败
#   PG_BIN_DIR     可选；psql / pg_restore 所在目录（不在 PATH 时使用）
#   KEEP_DRILL_DB  设为 1 则不删除临时库（仅排查问题用，用完须手动删除）
#
# 退出码：0 演练通过；非 0 任何一步失败（已清理临时库与下载文件）。
set -euo pipefail

LOCAL=0
if [ "${1:-}" = "--local" ]; then LOCAL=1; shift; fi
TARGET="${1:-}"
[ -n "$TARGET" ] || { sed -n '2,12p' "$0" >&2; exit 64; }

MC_ALIAS="${MC_ALIAS:-lurus-stage-minio}"
BACKUP_BUCKET="${BACKUP_BUCKET:-tally-backup}"
WORKDIR="${DRILL_WORKDIR:-/data/tmp}"
DRILL_DSN="${DRILL_DSN:-}"
SOURCE_DSN="${SOURCE_DSN:-}"
EXPECT_VERSION="${EXPECT_VERSION:-}"
KEEP_DRILL_DB="${KEEP_DRILL_DB:-0}"
PSQL="psql"; PG_RESTORE="pg_restore"
if [ -n "${PG_BIN_DIR:-}" ]; then PSQL="${PG_BIN_DIR}/psql"; PG_RESTORE="${PG_BIN_DIR}/pg_restore"; fi

die() { echo "演练失败：$*" >&2; exit 1; }
[ -n "$DRILL_DSN" ] || die "未设置 DRILL_DSN"
command -v "$PSQL" >/dev/null 2>&1 || [ -x "$PSQL" ] || [ -x "$PSQL.exe" ] || die "找不到 psql"

DB_NAME="tally_drill_$(date +%Y%m%d)"
# 把连接串里的库名换成临时库
drill_dsn=$(printf '%s' "$DRILL_DSN" | sed -E "s#(://[^/]*/)[^?]*#\1${DB_NAME}#")

mkdir -p "$WORKDIR"
JOB_DIR=$(mktemp -d "${WORKDIR%/}/drill.XXXXXX")
CREATED_DB=0

cleanup() {
  rc=$?
  set +e
  if [ "$CREATED_DB" = 1 ] && [ "$KEEP_DRILL_DB" != 1 ]; then
    "$PSQL" -d "$DRILL_DSN" -qAt -c "DROP DATABASE IF EXISTS ${DB_NAME} WITH (FORCE)" >/dev/null 2>&1 \
      && echo ">> 已删除临时库 ${DB_NAME}" || echo "!! 临时库 ${DB_NAME} 删除失败，请手动处理" >&2
  elif [ "$CREATED_DB" = 1 ]; then
    echo "!! 按要求保留临时库 ${DB_NAME}，用完请手动 DROP DATABASE" >&2
  fi
  rm -rf "$JOB_DIR"
  [ "$rc" = 0 ] || echo "演练未通过（退出码 $rc）" >&2
  exit "$rc"
}
trap cleanup EXIT

# 1) 取得备份文件
if [ "$LOCAL" = 1 ]; then
  [ -f "$TARGET" ] || die "本地备份文件不存在：$TARGET"
  DUMP="$TARGET"
else
  command -v mc >/dev/null 2>&1 || die "找不到 mc 命令"
  name="$TARGET"
  if [ "$name" = latest ]; then
    name=$(mc ls --json "${MC_ALIAS}/${BACKUP_BUCKET}/" \
      | sed -n 's/.*"key":"\([^"]*\.dump\)".*/\1/p' | sort | tail -n 1)
    [ -n "$name" ] || die "桶 ${BACKUP_BUCKET} 里没有 .dump 备份"
  fi
  echo ">> 下载 ${name} 到 ${JOB_DIR}"
  mc cp "${MC_ALIAS}/${BACKUP_BUCKET}/${name}" "${JOB_DIR}/${name}" >/dev/null || die "下载失败"
  DUMP="${JOB_DIR}/${name}"
fi
echo ">> 备份文件：$DUMP（$(wc -c < "$DUMP") 字节）"

# 2) 建临时库并恢复 tally schema
exists=$("$PSQL" -d "$DRILL_DSN" -qAt -c "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'")
[ -z "$exists" ] || die "临时库 ${DB_NAME} 已存在（今天已演练过？先确认后手动删除）"
"$PSQL" -d "$DRILL_DSN" -qAt -c "CREATE DATABASE ${DB_NAME}" >/dev/null || die "建临时库失败（账号需有建库权限）"
CREATED_DB=1
# 按 schema 导出的备份不含 CREATE SCHEMA 和扩展，需先建好；
# 线上三个扩展（向量、模糊匹配、加密）都装在 tally schema 里，临时库保持一致。
"$PSQL" -d "$drill_dsn" -qAt -c "CREATE SCHEMA tally" >/dev/null || die "建 tally schema 失败"
for ext in pgcrypto vector pg_trgm; do
  "$PSQL" -d "$drill_dsn" -qAt -c "CREATE EXTENSION \"${ext}\" WITH SCHEMA tally" >/dev/null     || die "建扩展 ${ext} 失败（账号权限不足或实例未安装该扩展）"
done
echo ">> 已建临时库 ${DB_NAME}，开始恢复"
"$PG_RESTORE" --no-owner --exit-on-error --schema=tally -d "$drill_dsn" "$DUMP" || die "pg_restore 失败"

# 3) 核对
q() { "$PSQL" -d "$1" -qAt -c "$2"; }
ver_row=$(q "$drill_dsn" "SELECT version || '|' || CASE WHEN dirty THEN 't' ELSE 'f' END FROM tally.schema_migrations LIMIT 1") || die "读取 schema_migrations 失败"
[ -n "$ver_row" ] || die "schema_migrations 为空"
version="${ver_row%%|*}"; dirty="${ver_row##*|}"
cnt() { q "$1" "SELECT count(*) FROM tally.$2" 2>/dev/null || echo "?"; }

printf '\n%-22s %-14s %s\n' "核对项" "恢复结果" "${SOURCE_DSN:+源库当前}"
printf '%-22s %-14s %s\n' "迁移版本" "$version" "${SOURCE_DSN:+$(q "$SOURCE_DSN" "SELECT version FROM tally.schema_migrations LIMIT 1" 2>/dev/null || echo '?')}"
printf '%-22s %-14s %s\n' "迁移未完成标记" "$dirty" "${SOURCE_DSN:+$(q "$SOURCE_DSN" "SELECT dirty FROM tally.schema_migrations LIMIT 1" 2>/dev/null || echo '?')}"
fail=0
for t in tenant bill_head product; do
  n=$(cnt "$drill_dsn" "$t")
  s=""; [ -z "$SOURCE_DSN" ] || s=$(cnt "$SOURCE_DSN" "$t")
  printf '%-22s %-14s %s\n' "行数 ${t}" "$n" "$s"
  if [ "$n" = "?" ]; then echo "!! 表 ${t} 无法读取" >&2; fail=1; fi
  if [ "$t" != bill_head ] && [ "$n" = 0 ]; then echo "!! 表 ${t} 为空，备份可能不完整" >&2; fail=1; fi
done
echo

[ "$dirty" = "f" ] || { echo "!! 迁移未完成标记为 ${dirty}，应为 f" >&2; fail=1; }
if [ -n "$EXPECT_VERSION" ] && [ "$version" != "$EXPECT_VERSION" ]; then
  echo "!! 迁移版本 ${version} 与期望 ${EXPECT_VERSION} 不符" >&2; fail=1
fi
[ "$fail" = 0 ] || die "核对不通过"
echo "演练通过：${DUMP##*/} 可正常恢复（迁移版本 ${version}）"
