/**
 * /ai — full-page AI assistant.
 *
 * The drawer (Cmd+J) stays as the inline mode for "while doing something
 * else". This page is the focus mode: bigger viewport, no surrounding UI
 * stealing attention. Both share the same MessageList + streamChat layer so
 * conversation history is consistent.
 */
import { AIPage } from "./ai-page"

export default function AIRoutePage() {
  return <AIPage />
}
