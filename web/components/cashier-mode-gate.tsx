"use client"

/**
 * Cashier mode: a screen cover for shared counters. While on, the back-office
 * pages (profit, receivables, purchase approval ...) are hidden and only the
 * POS and today's records stay. It is NOT access control: the API is still
 * reachable with the same login. It only covers THIS device (the flag lives in
 * localStorage); the shop keeps just the PIN. Turning it on/off needs the PIN.
 */

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react"
import { usePathname, useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Modal } from "@/components/ui/modal"
import { useProfile } from "@/lib/profile"
import { ApiError, extractApiError } from "@/lib/api/errors"
import {
  CASHIER_MODE_EVENT,
  getCashierMode,
  readCashierFlag,
  setCashierPin,
  verifyCashierPin,
  writeCashierFlag,
} from "@/lib/api/cashier-mode"

/** Paths a cashier may stay on: the POS, today's records and printouts. */
const CASHIER_PATHS = ["/pos", "/print"]

export function isCashierPath(pathname: string | null): boolean {
  if (!pathname) return false
  return CASHIER_PATHS.some((p) => pathname === p || pathname.startsWith(p + "/"))
}

interface CashierState {
  enabled: boolean | null
  hasPin: boolean
  /** Keys this device's flag; "" until the first read comes back. */
  tenantId: string
  /** The read failed and nothing is known yet: the page asks for a refresh. */
  failed: boolean
}

const UNKNOWN: CashierState = { enabled: null, hasPin: false, tenantId: "", failed: false }

/** One GET per page: reads the state, re-reads when the mode changes. `skip` = a provider already does it. */
function useCashierModeLoader(skip: boolean): CashierState {
  const [state, setState] = useState<CashierState>(UNKNOWN)
  useEffect(() => {
    if (skip) return
    const ctrl = new AbortController()
    const load = () =>
      getCashierMode(ctrl.signal)
        .then((s) => {
          if (ctrl.signal.aborted) return
          const tenantId = s.tenant_id ?? ""
          const hasPin = s.has_pin === true
          // No PIN (never set, or reset by support) means no way to leave the mode: drop a stale flag.
          if (tenantId !== "" && !hasPin) writeCashierFlag(tenantId, false)
          setState({ enabled: tenantId !== "" && hasPin && readCashierFlag(tenantId), hasPin, tenantId, failed: false })
        })
        .catch(() => {
          // A failed read is not "off": keep what we knew, or ask for a refresh.
          if (!ctrl.signal.aborted) setState((prev) => (prev.enabled === null ? { ...UNKNOWN, failed: true } : prev))
        })
    void load()
    window.addEventListener(CASHIER_MODE_EVENT, load)
    return () => {
      ctrl.abort()
      window.removeEventListener(CASHIER_MODE_EVENT, load)
    }
  }, [skip])
  return state
}

const CashierModeContext = createContext<CashierState | null>(null)

/** Shares one read of the mode with every gate below it (the dashboard layout mounts it once). */
export function CashierModeProvider({ children }: { children: ReactNode }) {
  const state = useCashierModeLoader(false)
  return <CashierModeContext.Provider value={state}>{children}</CashierModeContext.Provider>
}

/**
 * null = not known yet. `retailOnly` is for the back-office side: a shop that
 * cannot reach the POS is never treated as in cashier mode there, so it can't
 * be bounced around.
 */
export function useCashierMode(retailOnly = false): { enabled: boolean | null; hasPin: boolean; tenantId: string } {
  const { profileType } = useProfile()
  const shared = useContext(CashierModeContext)
  const own = useCashierModeLoader(shared !== null)
  // Without a provider (a page outside both layouts) a failed read stays best-effort "off";
  // under the dashboard and POS layouts the shared state is used and the failure is announced.
  const state = shared ?? (own.failed ? { ...own, enabled: false } : own)
  if (retailOnly && profileType !== "retail") return { enabled: false, hasPin: state.hasPin, tenantId: state.tenantId }
  return { enabled: state.enabled, hasPin: state.hasPin, tenantId: state.tenantId }
}

/**
 * Wraps the dashboard content. A retail shop sees nothing while the mode is unknown or on
 * (except the POS paths), so a back-navigation or a typed URL can't flash back-office pages.
 */
export function CashierModeContentGate({ children }: { children: ReactNode }) {
  const { profileType } = useProfile()
  const shared = useContext(CashierModeContext)
  const pathname = usePathname()
  if (profileType !== "retail" || shared === null || isCashierPath(pathname)) return <>{children}</>
  if (shared.enabled === false) return <>{children}</>
  if (shared.failed) {
    return (
      <div role="alert" className="p-6 text-sm text-muted-foreground">
        暂时读不到收银模式的状态，请刷新页面。
      </div>
    )
  }
  return null
}

/** Tells the POS header that the mode could not be read, since its buttons are hidden meanwhile. */
export function CashierModeReadNotice() {
  const shared = useContext(CashierModeContext)
  if (!shared?.failed) return null
  return (
    <span role="alert" className="px-3 py-1.5 text-xs text-muted-foreground">
      暂时读不到收银模式的状态，请刷新页面。
    </span>
  )
}

/** Renders children only when cashier mode is known to be off (nothing while still reading). */
export function HideInCashierMode({ children, retailOnly = false }: { children: ReactNode; retailOnly?: boolean }) {
  const { enabled } = useCashierMode(retailOnly)
  return enabled === false ? <>{children}</> : null
}

/** Sends a cashier who landed on a back-office page back to the POS. */
export function CashierModeRedirect() {
  const { enabled } = useCashierMode(true)
  const pathname = usePathname()
  const router = useRouter()
  useEffect(() => {
    if (enabled && !isCashierPath(pathname)) router.replace("/pos")
  }, [enabled, pathname, router])
  return null
}

interface ControlProps {
  /** "enter" is the owner's card on the dashboard; "exit" is the small POS button. */
  variant?: "enter" | "exit"
}

/** Turn-on / turn-off control with the PIN dialog. */
export function CashierModeControl({ variant = "enter" }: ControlProps) {
  const { enabled, hasPin, tenantId } = useCashierMode()
  const [open, setOpen] = useState(false)
  const [pin, setPin] = useState("")
  const [pin2, setPin2] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const turningOn = enabled !== true
  // The very first PIN is typed twice so a slip can't lock the owner out.
  const settingFirst = turningOn && !hasPin
  const close = useCallback(() => {
    setOpen(false)
    setPin("")
    setPin2("")
    setError(null)
  }, [])

  async function submit() {
    if (busy) return
    if (settingFirst && pin2.length === 0) {
      document.getElementById("cashier-pin2")?.focus()
      return
    }
    if (settingFirst && pin !== pin2) {
      setError("两次输入的密码不一样，请重新输入")
      return
    }
    setBusy(true)
    setError(null)
    try {
      // The shop keeps only the PIN; the switch itself is this device's.
      if (settingFirst) await setCashierPin(pin)
      else await verifyCashierPin(pin)
      writeCashierFlag(tenantId, turningOn)
      close()
      window.dispatchEvent(new Event(CASHIER_MODE_EVENT))
    } catch (e) {
      setError(e instanceof ApiError && e.status === 429 ? "试得太多了，请 5 分钟后再试" : extractApiError(e).message)
    } finally {
      setBusy(false)
    }
  }

  if (enabled === null) return null
  if (variant === "exit" && !enabled) return null
  if (variant === "enter" && enabled) return null

  const label = turningOn ? "开启收银模式" : "退出收银模式"
  return (
    <>
      {variant === "enter" ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-muted/50 px-4 py-3">
          <p className="text-sm text-muted-foreground">
            收银台的设备上，开启收银模式，只留收银台和今日结单，利润、欠款、采购审核都先藏起来。只在这台设备上生效，不影响你和其他同事。有收银员时请让他用自己的收银账号。
          </p>
          <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
            {label}
          </Button>
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="inline-flex min-h-[36px] items-center rounded-md px-3 py-1.5 text-xs text-zinc-500 transition-colors hover:bg-zinc-200 hover:text-zinc-900 dark:hover:bg-zinc-800 dark:hover:text-zinc-100"
        >
          {label}
        </button>
      )}
      <Modal
        open={open}
        onOpenChange={(o) => (o ? setOpen(true) : close())}
        title={label}
        description={
          turningOn
            ? hasPin
              ? "请输入你设置的密码。"
              : "第一次使用，请设置一个 4 到 6 位数字的密码，并输入两遍。以后开启、退出都要输入它，请记牢，只告诉自己人。"
            : "请输入老板设置的密码。"
        }
        footer={
          <>
            <Button variant="outline" onClick={close} disabled={busy}>
              取消
            </Button>
            <Button onClick={submit} disabled={busy || pin.length < 4 || (settingFirst && pin2.length < 4)}>
              确定
            </Button>
          </>
        }
      >
        <div className="mt-3 space-y-2">
          <label htmlFor="cashier-pin" className="text-sm font-medium">
            密码
          </label>
          <Input
            id="cashier-pin"
            type="password"
            inputMode="numeric"
            autoComplete="off"
            maxLength={6}
            value={pin}
            onChange={(e) => setPin(e.target.value.replace(/\D/g, ""))}
            onKeyDown={(e) => {
              if (e.key === "Enter") void submit()
            }}
          />
          {settingFirst && (
            <>
              <label htmlFor="cashier-pin2" className="text-sm font-medium">
                再输一遍密码
              </label>
              <Input
                id="cashier-pin2"
                type="password"
                inputMode="numeric"
                autoComplete="off"
                maxLength={6}
                value={pin2}
                onChange={(e) => setPin2(e.target.value.replace(/\D/g, ""))}
                onKeyDown={(e) => {
                  if (e.key === "Enter") void submit()
                }}
              />
            </>
          )}
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          {turningOn && (
            <p className="text-xs text-muted-foreground">
              说明：只在这台设备上生效，别的设备不受影响。这只是把界面上的入口藏起来，不是权限控制，并不能真正阻止同一个账号的人看到数据。
            </p>
          )}
          <p className="text-xs text-muted-foreground">忘记密码请联系客服，客服核实身份后可以帮你重置。</p>
        </div>
      </Modal>
    </>
  )
}
