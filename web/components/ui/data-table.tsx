"use client"

import type { ReactNode } from "react"
import {
  type ColumnDef,
  type RowData,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table"

import { Badge, type BadgeTone } from "@/components/ui/badge"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorBanner } from "@/components/ui/error-banner"
import { TableSkeleton } from "@/components/ui/table-skeleton"
import { formatCNY } from "@/lib/format"
import { cn } from "@/lib/utils"

// Per-column layout hints. Augments tanstack's ColumnMeta so column defs can say
// `meta: { align: "right" }` without leaving the type system.
declare module "@tanstack/react-table" {
  // Type params must mirror tanstack's ColumnMeta for declaration merging.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    align?: "left" | "center" | "right"
    cellClassName?: string
    headerClassName?: string
  }
}

function alignClass(align?: "left" | "center" | "right"): string {
  if (align === "right") return "text-right"
  if (align === "center") return "text-center"
  return "text-left"
}

/** Right-aligned monospace CNY cell. Use directly as a column `cell`. */
export function currencyCell(value: string | number | null | undefined) {
  return <span className="block text-right font-mono tabular-nums">{formatCNY(value)}</span>
}

/** Status badge cell. Pair with `lib/status.ts` tone maps. */
export function statusCell(label: ReactNode, tone: BadgeTone) {
  return <Badge tone={tone}>{label}</Badge>
}

// Stagger only the first rows, and cap the delay so a full page never feels slow.
const STAGGER_LIMIT = 30
const STAGGER_STEP_MS = 20
const STAGGER_MAX_STEPS = 12

interface DataTableProps<T> {
  columns: ColumnDef<T, any>[] // eslint-disable-line @typescript-eslint/no-explicit-any
  data: T[]
  loading?: boolean
  error?: string | null
  /** Stable row id; falls back to row index. */
  getRowId?: (row: T, index: number) => string
  /** Custom empty render. Defaults to a generic EmptyState. */
  empty?: ReactNode
  skeletonRows?: number
  /** Fade-and-rise the first paint of rows. Off by default to keep paging snappy. */
  animateRows?: boolean
  /** Whole-row click (e.g. open a detail panel). Action cells should stopPropagation. */
  onRowClick?: (row: T) => void
  className?: string
}

/**
 * DataTable is the single list-table renderer. It is render-only — built on
 * tanstack's core row model with no client paging/sorting/filtering — so
 * server-side pagination stays the source of truth and the bundle stays small.
 * Loading / error / empty states delegate to the existing primitives.
 */
export function DataTable<T>({
  columns,
  data,
  loading,
  error,
  getRowId,
  empty,
  skeletonRows = 5,
  animateRows = false,
  onRowClick,
  className,
}: DataTableProps<T>) {
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId,
  })

  if (loading) return <TableSkeleton rows={skeletonRows} cols={columns.length} />
  if (error) return <ErrorBanner hint="请稍后再试">{error}</ErrorBanner>
  if (data.length === 0) {
    return <>{empty ?? <EmptyState title="暂无数据" />}</>
  }

  return (
    <div className={cn("overflow-x-auto rounded-xl border border-border", className)}>
      <table className="w-full text-sm">
        <thead className="bg-muted/50 text-muted-foreground">
          {table.getHeaderGroups().map((hg) => (
            <tr key={hg.id}>
              {hg.headers.map((header) => {
                const meta = header.column.columnDef.meta
                return (
                  <th
                    key={header.id}
                    className={cn(
                      "px-4 py-2.5 font-medium",
                      alignClass(meta?.align),
                      meta?.headerClassName
                    )}
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </th>
                )
              })}
            </tr>
          ))}
        </thead>
        <tbody className="divide-y divide-border">
          {table.getRowModel().rows.map((row, i) => {
            const stagger = animateRows && i < STAGGER_LIMIT
            return (
              <tr
                key={row.id}
                onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                className={cn(
                  "transition-colors hover:bg-muted/30",
                  onRowClick && "cursor-pointer",
                  stagger && "animate-fade-in-up"
                )}
                style={
                  stagger
                    ? {
                        animationDelay: `${Math.min(i, STAGGER_MAX_STEPS) * STAGGER_STEP_MS}ms`,
                        animationFillMode: "backwards",
                      }
                    : undefined
                }
              >
                {row.getVisibleCells().map((cell) => {
                  const meta = cell.column.columnDef.meta
                  return (
                    <td
                      key={cell.id}
                      className={cn(
                        "px-4 py-2.5",
                        alignClass(meta?.align),
                        meta?.cellClassName
                      )}
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  )
                })}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
