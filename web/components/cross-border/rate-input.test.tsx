/**
 * Tests for RateInput component.
 */
import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import { RateInput } from "./rate-input"
import * as currencyApi from "@/lib/api/currency"

vi.mock("@/lib/api/currency")

describe("RateInput", () => {
  afterEach(() => vi.restoreAllMocks())

  it("disables input and shows '1' when currency is CNY", () => {
    render(<RateInput currency="CNY" value="1" onChange={vi.fn()} />)
    const input = screen.getByRole("textbox")
    expect(input).toBeDisabled()
    expect((input as HTMLInputElement).value).toBe("1")
  })

  it("auto-fetches rate when currency is USD", async () => {
    const mockResult: currencyApi.RateResult = { rate: "7.25", source: "manual" }
    vi.mocked(currencyApi.getRateOn).mockResolvedValue(mockResult)

    const onChange = vi.fn()
    render(<RateInput currency="USD" value="" onChange={onChange} />)

    await waitFor(() => expect(onChange).toHaveBeenCalledWith("7.25"))
    expect(currencyApi.getRateOn).toHaveBeenCalledWith("USD", "CNY", expect.any(String))
  })

  it("shows no_rate_found warning when no historical data", async () => {
    const fallback: currencyApi.RateResult = {
      rate: "1",
      source: "default",
      warning: "no_rate_found",
    }
    vi.mocked(currencyApi.getRateOn).mockResolvedValue(fallback)

    render(<RateInput currency="EUR" value="1" onChange={vi.fn()} />)

    await waitFor(() =>
      expect(screen.getByText(/未找到历史汇率/)).toBeTruthy()
    )
  })

  it("allows manual override of the rate", async () => {
    const mockResult: currencyApi.RateResult = { rate: "7.25", source: "manual" }
    vi.mocked(currencyApi.getRateOn).mockResolvedValue(mockResult)

    const onChange = vi.fn()
    render(<RateInput currency="USD" value="7.25" onChange={onChange} />)

    await waitFor(() => screen.getByRole("textbox"))

    const input = screen.getByRole("textbox")
    fireEvent.change(input, { target: { value: "7.30" } })
    expect(onChange).toHaveBeenCalledWith("7.30")
  })
})
