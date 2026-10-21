// topupLink: the platform's one top-up link (lurus-platform docs/integration-guide.md
// "Top-up link"), with the optional pre-filled amount and the page the owner returns
// to after paying. The base comes from the platform (overview.topup_url) — never
// hard-coded here, so a customer's own deployment links to its own platform.
export function topupLink(
  base: string,
  opts: { returnTo?: string; amount?: number } = {},
): string {
  try {
    const u = new URL(base);
    if (opts.amount && opts.amount > 0)
      u.searchParams.set("amount", String(Math.ceil(opts.amount * 100) / 100));
    if (opts.returnTo) u.searchParams.set("return_to", opts.returnTo);
    return u.toString();
  } catch {
    return base;
  }
}
