import { describe, expect, it } from "vitest";
import { topupLink } from "./topup";

describe("topupLink", () => {
  it("adds amount and return_to to the platform's link", () => {
    const u = new URL(
      topupLink("https://identity.lurus.cn/topup", {
        amount: 12.341,
        returnTo: "https://tally.lurus.cn/subscription",
      }),
    );
    expect(u.pathname).toBe("/topup");
    expect(u.searchParams.get("amount")).toBe("12.35");
    expect(u.searchParams.get("return_to")).toBe(
      "https://tally.lurus.cn/subscription",
    );
  });

  it("leaves the link alone when there is nothing to add or it does not parse", () => {
    expect(topupLink("https://identity.lurus.cn/topup")).toBe(
      "https://identity.lurus.cn/topup",
    );
    expect(topupLink("not a url", { returnTo: "x" })).toBe("not a url");
  });
});
