/**
 * API wrapper for the weekly summary ("Monday card") endpoint.
 *
 * Backend route (prefix /api/v1, proxied through /api/proxy):
 *   GET /weekly-summary
 *
 * Response fields use snake_case JSON; decimal amounts arrive as strings.
 */

const SERVER_BACKEND_URL =
  typeof window === "undefined"
    ? (process.env.BACKEND_URL ?? "http://tally-backend:18200")
    : ""

export interface WeeklySummary {
  replenish: {
    count: number
    amount_cny: string
  }
  oversell: {
    count: number
  }
  dead_stock: {
    count: number
  }
  // Last week's suggestion track record — optional: older backends omit it.
  suggestion_scorecard?: {
    suggested: number
    adopted: number
    missed_stockout: number
  }
  generated_at: string
}

/**
 * Server-side: GET /api/v1/weekly-summary
 * Requires the user's access_token from auth().
 * Degrades gracefully: returns null on any error so the card renders empty.
 */
export async function fetchWeeklySummary(accessToken: string): Promise<WeeklySummary | null> {
  if (!accessToken) return null
  try {
    const res = await fetch(`${SERVER_BACKEND_URL}/api/v1/weekly-summary`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      next: { revalidate: 300 },
    })
    if (!res.ok) return null
    return (await res.json()) as WeeklySummary
  } catch {
    return null
  }
}
