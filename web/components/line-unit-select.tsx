"use client"

const DEFAULT_CLASS =
  "w-full rounded-md border border-input bg-transparent px-2 py-1 text-base md:text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"

interface Option {
  id: string
  name: string
}

interface Props {
  value?: string
  options: Option[]
  ready: boolean
  failed?: boolean
  className?: string
  onChange: (unitId: string) => void
}

/** The unit cell of a bill line. While the list is loading the control stays disabled. */
export function LineUnitSelect({ value, options, ready, failed, className, onChange }: Props) {
  const current = options.find((o) => o.id === value)
  return (
    <select
      aria-label="单位"
      className={className ?? DEFAULT_CLASS}
      value={value ?? ""}
      disabled={!ready}
      onChange={(e) => onChange(e.target.value)}
    >
      {!ready ? (
        <option value={value ?? ""}>{current?.name || (value ? "…" : "加载中")}</option>
      ) : (
        <>
          <option value="">{failed ? "单位没加载出来" : "—"}</option>
          {options.map((u) => (
            <option key={u.id} value={u.id}>
              {u.name}
            </option>
          ))}
        </>
      )}
    </select>
  )
}
