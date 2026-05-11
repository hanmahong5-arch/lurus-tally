"use client"

import { useEffect, type DependencyList } from "react"

/**
 * useAbortableEffect runs an effect callback with a fresh AbortSignal and a
 * `cancelled` flag, and aborts the controller (plus flips the flag) whenever
 * the dependencies change or the component unmounts.
 *
 * Use this for list pages whose effects fire HTTP GETs: it eliminates the
 * "Can't perform a React state update on an unmounted component" warning
 * and prevents stale responses from clobbering newer ones during rapid filter
 * changes.
 *
 *   useAbortableEffect((signal, isCancelled) => {
 *     listProducts({ q, signal })
 *       .then((res) => { if (!isCancelled()) setItems(res.items) })
 *   }, [q])
 */
export function useAbortableEffect(
  effect: (signal: AbortSignal, isCancelled: () => boolean) => void | (() => void),
  deps: DependencyList,
): void {
  useEffect(() => {
    const controller = new AbortController()
    let cancelled = false
    const cleanup = effect(controller.signal, () => cancelled)
    return () => {
      cancelled = true
      controller.abort()
      if (typeof cleanup === "function") cleanup()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
}
