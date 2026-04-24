/**
 * Tests for CurrencySelector component.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import { CurrencySelector } from "./currency-selector"
import * as currencyApi from "@/lib/api/currency"

vi.mock("@/lib/api/currency")

const mockCurrencies: currencyApi.Currency[] = [
  { code: "CNY", name: "人民币", symbol: "¥", enabled: true },
  { code: "USD", name: "美元", symbol: "$", enabled: true },
  { code: "EUR", name: "欧元", symbol: "€", enabled: true },
]

describe("CurrencySelector", () => {
  beforeEach(() => {
    vi.mocked(currencyApi.getCurrencies).mockResolvedValue(mockCurrencies)
  })
  afterEach(() => vi.restoreAllMocks())

  it("calls getCurrencies on mount", async () => {
    render(<CurrencySelector value="CNY" onChange={vi.fn()} />)
    await waitFor(() => expect(currencyApi.getCurrencies).toHaveBeenCalledOnce())
  })

  it("renders currency options after loading", async () => {
    render(<CurrencySelector value="CNY" onChange={vi.fn()} />)
    await waitFor(() => {
      expect(screen.getByText("CNY — 人民币")).toBeTruthy()
      expect(screen.getByText("USD — 美元")).toBeTruthy()
    })
  })

  it("calls onChange when user selects USD", async () => {
    const onChange = vi.fn()
    render(<CurrencySelector value="CNY" onChange={onChange} />)

    await waitFor(() => screen.getByText("USD — 美元"))

    const select = screen.getByRole("combobox")
    fireEvent.change(select, { target: { value: "USD" } })
    expect(onChange).toHaveBeenCalledWith("USD")
  })
})
