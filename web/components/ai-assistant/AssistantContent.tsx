"use client"

import { Fragment, type ReactNode } from "react"

/**
 * AssistantContent renders a finished assistant message as lightly-formatted
 * rich content — inventory/sales answers come back as markdown (bold figures,
 * bullet findings, pipe tables of SKUs). A dependency-free block parser turns
 * those into Linear/Stripe-style result blocks: bold numbers stand out, lists
 * read cleanly, and pipe tables become scannable result cards.
 *
 * Kept deliberately small (no markdown library — Bun-only repo, and we only
 * need the handful of constructs the assistant actually emits). While a message
 * is still streaming the caller renders raw text with a cursor instead, so this
 * only ever sees complete, well-formed blocks.
 */
export function AssistantContent({ text }: { text: string }) {
  const blocks = parseBlocks(text)
  return (
    <div className="space-y-2 text-sm leading-relaxed">
      {blocks.map((b, i) => (
        <Fragment key={i}>{renderBlock(b)}</Fragment>
      ))}
    </div>
  )
}

type Block =
  | { kind: "table"; header: string[]; rows: string[][] }
  | { kind: "ul"; items: string[] }
  | { kind: "ol"; items: string[] }
  | { kind: "p"; text: string }

const isTableRow = (l: string) => /^\s*\|.*\|\s*$/.test(l)
const isDivider = (l: string) => /^\s*\|?[\s:|-]+\|?\s*$/.test(l) && l.includes("-")
const bulletRe = /^\s*[-*]\s+(.*)$/
const orderedRe = /^\s*\d+[.)]\s+(.*)$/

function parseBlocks(text: string): Block[] {
  const lines = text.replace(/\r/g, "").split("\n")
  const blocks: Block[] = []
  let i = 0

  while (i < lines.length) {
    const line = lines[i]

    if (line.trim() === "") {
      i++
      continue
    }

    // Pipe table: header row, divider row, then body rows.
    if (isTableRow(line) && i + 1 < lines.length && isDivider(lines[i + 1])) {
      const header = splitRow(line)
      const rows: string[][] = []
      i += 2
      while (i < lines.length && isTableRow(lines[i]) && !isDivider(lines[i])) {
        rows.push(splitRow(lines[i]))
        i++
      }
      blocks.push({ kind: "table", header, rows })
      continue
    }

    // Bulleted list.
    if (bulletRe.test(line)) {
      const items: string[] = []
      while (i < lines.length && bulletRe.test(lines[i])) {
        items.push(lines[i].replace(bulletRe, "$1"))
        i++
      }
      blocks.push({ kind: "ul", items })
      continue
    }

    // Numbered list.
    if (orderedRe.test(line)) {
      const items: string[] = []
      while (i < lines.length && orderedRe.test(lines[i])) {
        items.push(lines[i].replace(orderedRe, "$1"))
        i++
      }
      blocks.push({ kind: "ol", items })
      continue
    }

    // Paragraph: gather until blank line or a structural line.
    const para: string[] = []
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !isTableRow(lines[i]) &&
      !bulletRe.test(lines[i]) &&
      !orderedRe.test(lines[i])
    ) {
      para.push(lines[i])
      i++
    }
    blocks.push({ kind: "p", text: para.join(" ") })
  }

  return blocks
}

function splitRow(line: string): string[] {
  return line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((c) => c.trim())
}

function renderBlock(b: Block): ReactNode {
  switch (b.kind) {
    case "table":
      return (
        <div className="overflow-x-auto rounded-lg border border-border bg-background">
          <table className="w-full border-collapse text-xs">
            <thead>
              <tr className="border-b border-border bg-muted/50">
                {b.header.map((h, i) => (
                  <th key={i} className="px-2.5 py-1.5 text-left font-semibold text-foreground">
                    {inline(h)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {b.rows.map((row, ri) => (
                <tr key={ri} className="border-b border-border/60 last:border-0">
                  {row.map((cell, ci) => (
                    <td
                      key={ci}
                      className={`px-2.5 py-1.5 ${ci === 0 ? "text-foreground" : "font-mono tabular-nums text-muted-foreground"}`}
                    >
                      {inline(cell)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    case "ul":
      return (
        <ul className="space-y-1">
          {b.items.map((it, i) => (
            <li key={i} className="flex gap-2">
              <span aria-hidden="true" className="mt-1.5 h-1 w-1 flex-shrink-0 rounded-full bg-primary/60" />
              <span className="flex-1">{inline(it)}</span>
            </li>
          ))}
        </ul>
      )
    case "ol":
      return (
        <ol className="space-y-1">
          {b.items.map((it, i) => (
            <li key={i} className="flex gap-2">
              <span aria-hidden="true" className="font-mono text-xs text-primary/70">{i + 1}.</span>
              <span className="flex-1">{inline(it)}</span>
            </li>
          ))}
        </ol>
      )
    case "p":
      return <p className="whitespace-pre-wrap break-words">{inline(b.text)}</p>
  }
}

/**
 * inline renders **bold** and `code` spans; everything else is plain text.
 * Numbers wrapped in ** (the assistant bolds key figures) get tabular-nums so
 * amounts and quantities line up.
 */
function inline(text: string): ReactNode {
  const parts: ReactNode[] = []
  const re = /(\*\*[^*]+\*\*|`[^`]+`)/g
  let last = 0
  let m: RegExpExecArray | null
  let key = 0
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) parts.push(text.slice(last, m.index))
    const tok = m[0]
    if (tok.startsWith("**")) {
      parts.push(
        <strong key={key++} className="font-semibold tabular-nums text-foreground">
          {tok.slice(2, -2)}
        </strong>
      )
    } else {
      parts.push(
        <code key={key++} className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]">
          {tok.slice(1, -1)}
        </code>
      )
    }
    last = m.index + tok.length
  }
  if (last < text.length) parts.push(text.slice(last))
  return parts.length ? parts : text
}
