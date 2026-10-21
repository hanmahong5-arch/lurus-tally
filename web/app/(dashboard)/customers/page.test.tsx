import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import { ApiError } from "@/lib/api/errors"

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const api = vi.hoisted(() => ({
  listCustomers: vi.fn(),
  createCustomer: vi.fn(),
  getCustomer: vi.fn(),
}))
vi.mock("@/lib/api/customers", async (orig) => ({
  ...(await orig<typeof import("@/lib/api/customers")>()),
  ...api,
}))

import CustomersPage from "./page"

describe("CustomersPage duplicate create", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.listCustomers.mockResolvedValue({ items: [], total: 0 })
  })

  it("409 shows the message and opens the existing profile by id", async () => {
    api.createCustomer.mockRejectedValue(
      new ApiError(409, "duplicate_customer_name", "已存在同名客户「王五」", { existing_id: "c-9" })
    )
    api.getCustomer.mockResolvedValue({ id: "c-9", name: "王五", code: "C-0009", mobile: "13800000000" })
    render(<CustomersPage />)
    await waitFor(() => expect(api.listCustomers).toHaveBeenCalled())

    fireEvent.click(screen.getByText("+ 新增客户"))
    fireEvent.change(screen.getByLabelText(/名称/), { target: { value: "王五" } })
    fireEvent.click(screen.getByText("保存"))

    expect(await screen.findByText("已存在同名客户「王五」")).toBeInTheDocument()
    fireEvent.click(screen.getByText("查看已有客户"))

    await waitFor(() => expect(api.getCustomer).toHaveBeenCalledWith("c-9"))
    expect(await screen.findByText("C-0009")).toBeInTheDocument()
    expect(screen.queryByText("查看已有客户")).not.toBeInTheDocument()
  })
})
