/** Browser storage keys of personal leftovers; shared so wiping a device cannot drift from where they are written. */
export const COPY_BILL_PREFIX = "tally:copy-bill:"
export const AI_HISTORY_KEY = "tally_ai_history"
/** Cookie the sign-in page reads so a switch still forces the account prompt after a redirect that drops the query. */
export const SWITCH_COOKIE = "tally-switch"
/** Written on sign-out; other tabs of the same browser see the change and drop their cart. */
export const SIGNOUT_SIGNAL_KEY = "tally:signed-out-at"
/** localStorage prefix of a refund that failed to book; the sale id follows. */
export const PENDING_REFUND_PREFIX = "tally.pending-refund."
/** Window event fired when a sign-out request failed; the till page saves its in-memory cart again. */
export const SIGNOUT_FAILED_EVENT = "tally:signout-failed"
/** localStorage key of this device's random id (survives sign-out; names offline checkouts). */
export const DEVICE_ID_KEY = "tally:device-id"
/** localStorage key of the monotonically increasing local checkout sequence of this device. */
export const CHECKOUT_SEQ_KEY = "tally:checkout-seq"
