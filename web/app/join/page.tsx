import { Suspense } from "react"

import { JoinClient } from "./join-client"

// /join is the landing page of a shop invite link. The middleware lets a
// signed-in person without a shop reach it (they have no profile yet).
export default function JoinPage() {
  return (
    <Suspense fallback={null}>
      <JoinClient />
    </Suspense>
  )
}
