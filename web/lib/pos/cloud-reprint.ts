import { markCloudPrinted, mutateQueue, type QueueEntry } from "@/lib/pos/checkout-queue"
import { readCloudPrintInfo } from "@/hooks/use-cloud-print"
import { sendCloudReceipt } from "@/lib/pos/cloud-print-send"

export interface ReprintDeps {
  /** 本机缓存的云打印能力；默认读 use-cloud-print 的缓存。 */
  info?: () => { available: boolean; configured: boolean; autoPrint: boolean } | null
  send?: (billId: string, opts: { tenantId?: string; force?: boolean }) => Promise<boolean>
}

/**
 * 离线单联网上传成功后，店铺开了云打印自动打印就补发一次。
 * 先把 cloud_printed 写进台账再发：宁可极端情况下少打一张，也不重复出纸；每条只会发一次。
 * 返回是否发出了请求。
 */
export async function reprintAfterReplay(
  key: string,
  entry: QueueEntry,
  opts: { tenantId?: string } & ReprintDeps = {},
): Promise<boolean> {
  if (entry.cloud_printed || entry.status !== "done" || !entry.server_bill_id) return false
  const info = (opts.info ?? readCloudPrintInfo)()
  if (!info || !info.available || !info.configured || !info.autoPrint) return false
  let first = false
  await mutateQueue(key, (es) =>
    es.map((e) => {
      if (e.client_ref !== entry.client_ref || e.cloud_printed) return e
      first = true
      return markCloudPrinted([e], e.client_ref)[0]
    }),
  )
  if (!first) return false
  await (opts.send ?? sendCloudReceipt)(entry.server_bill_id, { tenantId: opts.tenantId, force: true })
  return true
}
