"use client"

import { useEffect, useId, useRef, useState } from "react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { extractApiError } from "@/lib/api/errors"

export interface EntityOption {
  id: string
  name: string
  /** Secondary text shown after the name (code, phone, ...). */
  sub?: string
  /** Set by create() when it resolved to an already-existing entity, so no "新建" toast. */
  reused?: boolean
}

interface Props {
  /** Selected entity id ("" when nothing is selected). */
  value: string
  /** Display name of the selected entity. */
  label: string
  onSelect: (option: EntityOption) => void
  /** Called when the user empties the field and leaves it (optional fields). */
  onClear?: () => void
  search: (q: string, signal: AbortSignal) => Promise<EntityOption[]>
  /**
   * Creates a new entity from the typed name and returns it. When omitted the
   * picker is select-only. A successful create writes the entity into its own
   * catalogue (product / supplier / customer), so it is reusable afterwards.
   */
  create?: (name: string) => Promise<EntityOption>
  /** Noun used in UI copy, e.g. "商品" / "供应商" / "客户". */
  entityLabel: string
  placeholder?: string
  disabled?: boolean
  className?: string
  debounceMs?: number
  id?: string
}

type Row = { kind: "option"; option: EntityOption } | { kind: "create"; name: string }

/**
 * EntityCombobox — search-or-create picker for bill forms.
 *
 * Typing searches the catalogue; when no option matches the typed name exactly
 * a "新建…" row is offered so a missing product / supplier / customer can be
 * created in place without leaving the bill. Leaving the field without picking
 * restores the previously selected label.
 */
export function EntityCombobox({
  value,
  label,
  onSelect,
  onClear,
  search,
  create,
  entityLabel,
  placeholder,
  disabled = false,
  className,
  debounceMs = 200,
  id,
}: Props) {
  const [text, setText] = useState(label)
  const [open, setOpen] = useState(false)
  const [options, setOptions] = useState<EntityOption[]>([])
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [highlight, setHighlight] = useState(0)
  const focused = useRef(false)
  const listboxId = useId()

  // Follow external selection changes while the user is not editing.
  useEffect(() => {
    if (!focused.current) setText(label)
  }, [label, value])

  // Debounced search for the current query.
  const [pending, setPending] = useState<{ q: string; seq: number } | null>(null)
  useEffect(() => {
    if (pending === null) return
    const ctrl = new AbortController()
    const timer = setTimeout(async () => {
      setLoading(true)
      try {
        const res = await search(pending.q, ctrl.signal)
        if (!ctrl.signal.aborted) {
          setOptions(res)
          setHighlight(0)
        }
      } catch {
        if (!ctrl.signal.aborted) setOptions([])
      } finally {
        if (!ctrl.signal.aborted) setLoading(false)
      }
    }, debounceMs)
    return () => {
      clearTimeout(timer)
      ctrl.abort()
    }
  }, [pending, search, debounceMs])

  function runSearch(q: string) {
    // Mark loading immediately: the "新建" row must not appear until results
    // for this exact query are back, or a fast click could duplicate an entity
    // that already exists in the catalogue.
    setLoading(true)
    // seq forces a fresh search even when q repeats (e.g. refocus with "").
    setPending((p) => ({ q, seq: (p?.seq ?? 0) + 1 }))
  }

  const trimmed = text.trim()
  const normalize = (v: string) => v.trim().replace(/\s+/g, " ").toLowerCase()
  const exact = options.some((o) => normalize(o.name) === normalize(trimmed))
  const rows: Row[] = options.map((option) => ({ kind: "option", option }))
  if (create && trimmed && !exact && !loading) rows.push({ kind: "create", name: trimmed })

  async function pick(row: Row) {
    setError(null)
    if (row.kind === "option") {
      finish(row.option)
      return
    }
    if (!create || creating) return
    setCreating(true)
    try {
      const created = await create(row.name)
      if (!created.reused) toast.success(`已新建${entityLabel}「${created.name}」，并加入${entityLabel}档案`)
      finish(created)
    } catch (e) {
      setError(`创建失败：${extractApiError(e).message}`)
    } finally {
      setCreating(false)
    }
  }

  function finish(option: EntityOption) {
    onSelect(option)
    setText(option.name)
    setOpen(false)
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (!open) return
    if (e.key === "ArrowDown") {
      e.preventDefault()
      setHighlight((h) => Math.min(h + 1, rows.length - 1))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      setHighlight((h) => Math.max(h - 1, 0))
    } else if (e.key === "Enter") {
      // Never let Enter submit the surrounding bill form while picking.
      e.preventDefault()
      const row = rows[highlight]
      if (row) void pick(row)
    } else if (e.key === "Escape") {
      setOpen(false)
      setText(label)
    }
  }

  return (
    <div className={cn("relative", className)}>
      <input
        id={id}
        role="combobox"
        aria-expanded={open}
        aria-controls={listboxId}
        aria-autocomplete="list"
        className="w-full rounded-md border border-input bg-transparent px-2 py-1 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"
        value={text}
        placeholder={placeholder ?? `搜索或新建${entityLabel}`}
        disabled={disabled}
        onFocus={() => {
          focused.current = true
          setOpen(true)
          runSearch(text === label ? "" : text.trim())
        }}
        onBlur={() => {
          focused.current = false
          setOpen(false)
          setError(null)
          if (onClear && text.trim() === "" && value) onClear()
          else setText(label)
        }}
        onChange={(e) => {
          setText(e.target.value)
          setOpen(true)
          setError(null)
          runSearch(e.target.value.trim())
        }}
        onKeyDown={onKeyDown}
      />
      {open && (
        <ul
          id={listboxId}
          role="listbox"
          className="absolute z-50 mt-1 max-h-64 w-full min-w-56 overflow-auto rounded-md border border-border bg-popover p-1 text-sm shadow-md"
        >
          {loading && <li className="px-2 py-1.5 text-muted-foreground">搜索中…</li>}
          {!loading && rows.length === 0 && (
            <li className="px-2 py-1.5 text-muted-foreground">无匹配{entityLabel}</li>
          )}
          {rows.map((row, idx) => (
            <li
              key={row.kind === "option" ? row.option.id : "__create"}
              role="option"
              aria-selected={idx === highlight}
              className={cn(
                "cursor-pointer rounded px-2 py-1.5",
                idx === highlight && "bg-muted",
                row.kind === "create" && "text-primary"
              )}
              onMouseEnter={() => setHighlight(idx)}
              // mousedown (not click) so the pick lands before the input blurs.
              onMouseDown={(e) => {
                e.preventDefault()
                void pick(row)
              }}
            >
              {row.kind === "option" ? (
                <>
                  <span>{row.option.name}</span>
                  {row.option.sub && (
                    <span className="ml-2 text-xs text-muted-foreground">{row.option.sub}</span>
                  )}
                </>
              ) : (
                <span>{creating ? "创建中…" : `+ 新建${entityLabel}「${row.name}」`}</span>
              )}
            </li>
          ))}
          {error && <li className="px-2 py-1.5 text-destructive">{error}</li>}
        </ul>
      )}
    </div>
  )
}
