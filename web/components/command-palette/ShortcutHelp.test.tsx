import { describe, it, expect } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"

import { ShortcutHelp } from "./ShortcutHelp"

function open() {
  fireEvent.keyDown(window, { key: "?" })
}

describe("ShortcutHelp", () => {
  it("lists the AI drawer shortcut when the assistant is on", () => {
    render(<ShortcutHelp aiEnabled />)
    open()
    expect(screen.getByText("AI 助手抽屉")).toBeInTheDocument()
  })

  it("drops every AI row when the assistant is off", () => {
    render(<ShortcutHelp aiEnabled={false} />)
    open()
    expect(screen.getByText("键盘快捷键")).toBeInTheDocument()
    expect(screen.queryByText(/⌘J/)).toBeNull()
    expect(screen.queryByText(/AI/)).toBeNull()
  })
})
