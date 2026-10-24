import type { MetadataRoute } from "next"

// Dark is the default theme, so the splash/background matches the dark canvas.
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "进销存",
    short_name: "进销存",
    description: "进销存，开单记账查库存",
    // PWA installs are aimed at cashiers on /pos; back-office users still reach
    // the dashboard via in-app links. Non-retail profiles are redirected away
    // from /pos by middleware.
    start_url: "/pos",
    scope: "/",
    display: "standalone",
    theme_color: "#0a0a0a",
    background_color: "#0a0a0a",
    icons: [
      { src: "/icons/icon-192.png", sizes: "192x192", type: "image/png", purpose: "any" },
      { src: "/icons/icon-512.png", sizes: "512x512", type: "image/png", purpose: "any" },
      { src: "/icons/icon-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  }
}
