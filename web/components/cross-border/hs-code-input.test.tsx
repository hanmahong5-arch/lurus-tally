/**
 * Tests for HsCodeInput component.
 */
import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { HsCodeInput } from "./hs-code-input"

describe("HsCodeInput", () => {
  afterEach(() => vi.restoreAllMocks())

  it("renders with placeholder text", () => {
    render(<HsCodeInput value="" onChange={vi.fn()} />)
    expect(screen.getByPlaceholderText("输入 HS 编码（6/8/10 位）")).toBeTruthy()
  })

  it("filters non-digit input on change", () => {
    const onChange = vi.fn()
    render(<HsCodeInput value="" onChange={onChange} />)

    const input = screen.getByRole("textbox")
    // Simulate paste of alphanumeric string
    fireEvent.change(input, { target: { value: "abc123def" } })

    // onChange should receive only digits
    expect(onChange).toHaveBeenCalledWith("123")
  })

  it("accepts valid 6-digit input without warning", () => {
    render(<HsCodeInput value="847130" onChange={vi.fn()} />)
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("accepts valid 10-digit input without warning", () => {
    render(<HsCodeInput value="8471300000" onChange={vi.fn()} />)
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("shows warning for 7-digit input but does not block submit", () => {
    render(<HsCodeInput value="8471300" onChange={vi.fn()} />)
    const alert = screen.getByRole("alert")
    expect(alert).toBeTruthy()
    expect(alert.textContent).toMatch(/保存不受影响/)
  })

  it("shows warning for non-standard lengths (e.g. 4 digits)", () => {
    render(<HsCodeInput value="8471" onChange={vi.fn()} />)
    expect(screen.getByRole("alert")).toBeTruthy()
  })

  it("no warning for empty input", () => {
    render(<HsCodeInput value="" onChange={vi.fn()} />)
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("does not call onChange for non-digit characters", () => {
    const onChange = vi.fn()
    render(<HsCodeInput value="123" onChange={onChange} />)

    const input = screen.getByRole("textbox")
    // Simulate change with a non-digit value - the component should strip non-digits
    fireEvent.change(input, { target: { value: "123a" } })
    // onChange should be called with "123" (non-digit stripped)
    expect(onChange).toHaveBeenCalledWith("123")
  })
})
