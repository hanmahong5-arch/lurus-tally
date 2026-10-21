# 客服手册：找回误删的基础资料

适用：店铺误删了仓库、供应商、苗木字典、项目，需要在当天找回。这些资料都是软删除（deleted_at 有值），数据还在库里，只是列表看不到。

本手册只含只读 SELECT 查询。找回动作通过产品自带的 restore 接口完成，不直接改库。

## 0. 前置

- 连接数据库需用能绕过行级隔离（RLS）的运维账号；若只有普通应用账号，每次连接先执行 `SELECT set_config('app.tenant_id', '<租户 id>', false);`，之后的查询才看得到该店铺的数据。
- 所有表在 `tally` 模式下。
- 只在店铺负责人确认要找回之后再操作，并记下工单号。

## 1. 按店铺名查租户 id

```sql
SELECT id, name, created_at
FROM tally.tenant
WHERE name ILIKE '%店铺名关键字%'
ORDER BY created_at DESC
LIMIT 20;
```

同名多条时，与店铺负责人核对创建时间再确定。

## 2. 查已删除的记录 id

把 `<租户 id>` 换成上一步查到的 id。结果按删除时间倒序，最近删的在最上面。

仓库：

```sql
SELECT id, code, name, deleted_at
FROM tally.warehouse
WHERE tenant_id = '<租户 id>' AND deleted_at IS NOT NULL
ORDER BY deleted_at DESC
LIMIT 50;
```

供应商：

```sql
SELECT id, name, deleted_at
FROM tally.supplier
WHERE tenant_id = '<租户 id>' AND deleted_at IS NOT NULL
ORDER BY deleted_at DESC
LIMIT 50;
```

苗木字典：

```sql
SELECT id, name, deleted_at
FROM tally.nursery_dict
WHERE tenant_id = '<租户 id>' AND deleted_at IS NOT NULL
ORDER BY deleted_at DESC
LIMIT 50;
```

项目：

```sql
SELECT id, name, deleted_at
FROM tally.project
WHERE tenant_id = '<租户 id>' AND deleted_at IS NOT NULL
ORDER BY deleted_at DESC
LIMIT 50;
```

## 3. 调用 restore 接口找回

| 资料 | 方法与路径 |
| --- | --- |
| 仓库 | `POST /api/v1/warehouses/:id/restore` |
| 供应商 | `POST /api/v1/suppliers/:id/restore` |
| 苗木字典 | `POST /api/v1/nursery-dict/:id/restore` |
| 项目 | `POST /api/v1/projects/:id/restore` |

- `:id` 填第 2 步查到的记录 id，请求体为空。
- 权限：调用者需要“基础资料维护”权限（catalog.write），老板、店长、店员角色有；收银员、仓管等没有。请让店铺负责人本人（或具备该角色的成员）登录后调用，不要用别人的身份。
- 生产与准生产环境按各自域名调用，带上该成员正常登录得到的凭证（Authorization 请求头）。示例：

```bash
curl -X POST "https://<产品域名>/api/v1/warehouses/<记录 id>/restore" \
  -H "Authorization: Bearer <该成员的凭证>"
```

- 返回 200 并带回恢复后的记录，即成功；返回 404 表示该 id 不属于这家店铺或已不是删除状态，请回到第 1、2 步核对租户和 id。

## 4. 核对

恢复后让店铺负责人刷新页面确认记录回来了。也可再用第 2 步的查询确认该 id 已不在结果里：

```sql
SELECT id, name, deleted_at
FROM tally.warehouse
WHERE id = '<记录 id>';
```

（其他三张表把表名换成 supplier、nursery_dict、project。）deleted_at 为空即已找回。

## 5. 不能用本手册解决的情况

- 单据（采购单、销售单）被作废：走单据自己的恢复流程，不在本手册范围。
- 超过数据保留期或已被清理的记录：不要手工改库，转交开发处理。
