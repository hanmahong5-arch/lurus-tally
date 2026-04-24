export default function ExchangeRatesLoading() {
  return (
    <div className="p-6 max-w-4xl mx-auto space-y-8">
      {/* Header skeleton */}
      <div className="flex items-center justify-between">
        <div className="space-y-2">
          <div className="h-6 w-32 rounded-md bg-muted animate-pulse" />
          <div className="h-4 w-48 rounded-md bg-muted animate-pulse" />
        </div>
        <div className="h-9 w-28 rounded-lg bg-muted animate-pulse" />
      </div>

      {/* Table skeleton */}
      <section className="space-y-2">
        <div className="h-4 w-24 rounded bg-muted animate-pulse" />
        {[...Array(5)].map((_, i) => (
          <div key={i} className="h-10 rounded-md bg-muted/50 animate-pulse" />
        ))}
      </section>

      {/* Chart skeleton */}
      <section className="space-y-2">
        <div className="h-4 w-32 rounded bg-muted animate-pulse" />
        <div className="h-40 rounded-xl bg-muted/50 animate-pulse" />
      </section>
    </div>
  )
}
