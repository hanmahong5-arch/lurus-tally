import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"

vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))

import { AINotConfigured } from "./AINotConfigured"

describe("AINotConfigured in the trial period", () => {
  it("says 试用期暂未开放 and shows no upgrade link", () => {
    const { container } = render(<AINotConfigured reason="not_entitled" />)
    expect(container.textContent).toContain("试用期暂未开放")
    expect(screen.queryByTestId("ai-upgrade-link")).toBeNull()
    expect(container.textContent).not.toContain("升级")
  })
})
