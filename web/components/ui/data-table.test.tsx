import { afterEach, describe, expect, it, vi } from "vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import type { ColumnDef } from "@tanstack/react-table"

import { DataTable } from "./data-table"

interface Row {
  id: string
  name: string
}

const columns: ColumnDef<Row>[] = [
  { id: "name", header: "名称", cell: ({ row }) => <span>{row.original.name}</span> },
]
const data: Row[] = [
  { id: "1", name: "甲" },
  { id: "2", name: "乙" },
]

function mockViewport(mobile: boolean) {
  window.matchMedia = vi.fn().mockImplementation(() => ({
    matches: mobile,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })) as unknown as typeof window.matchMedia
}

describe("DataTable mobileCard", () => {
  afterEach(() => {
    // @ts-expect-error 还原为无 matchMedia 的测试环境
    delete window.matchMedia
  })

  it("手机视口：渲染 md:hidden 卡片容器", () => {
    mockViewport(true)
    const { container } = render(
      <DataTable
        columns={columns}
        data={data}
        getRowId={(r) => r.id}
        mobileCard={(r) => <div data-testid="card">{r.name}</div>}
      />
    )
    const wrap = screen.getByTestId("data-table-mobile")
    expect(wrap.className).toContain("md:hidden")
    expect(screen.getAllByTestId("card")).toHaveLength(2)
    expect(container.querySelector("table")).toBeNull()
  })

  it("桌面视口：只渲染 hidden md:block 包裹的表格", () => {
    mockViewport(false)
    const { container } = render(
      <DataTable columns={columns} data={data} mobileCard={(r) => <div data-testid="card">{r.name}</div>} />
    )
    expect(screen.queryByTestId("data-table-mobile")).toBeNull()
    expect(container.querySelector("div.hidden table")).not.toBeNull()
  })

  it("卡片点击触发 onRowClick", () => {
    mockViewport(true)
    const onRowClick = vi.fn()
    render(
      <DataTable
        columns={columns}
        data={data}
        getRowId={(r) => r.id}
        onRowClick={onRowClick}
        mobileCard={(r) => <div data-testid="card">{r.name}</div>}
      />
    )
    fireEvent.click(screen.getAllByTestId("card")[1])
    expect(onRowClick).toHaveBeenCalledWith(data[1])
  })

  it("无 mobileCard 时不渲染卡片容器", () => {
    const { container } = render(<DataTable columns={columns} data={data} />)
    expect(screen.queryByTestId("data-table-mobile")).toBeNull()
    expect(container.querySelector("table")).not.toBeNull()
  })

  it("空态仍渲染（带 mobileCard）", () => {
    render(<DataTable columns={columns} data={[]} mobileCard={() => <div />} empty={<p>没有数据</p>} />)
    expect(screen.getByText("没有数据")).toBeTruthy()
    expect(screen.queryByTestId("data-table-mobile")).toBeNull()
  })

  it("加载态与错误态仍渲染（带 mobileCard）", () => {
    const { rerender } = render(
      <DataTable columns={columns} data={[]} loading mobileCard={() => <div />} />
    )
    expect(screen.queryByTestId("data-table-mobile")).toBeNull()
    rerender(<DataTable columns={columns} data={[]} error="出错了" mobileCard={() => <div />} />)
    expect(screen.getByText("出错了")).toBeTruthy()
  })
})
