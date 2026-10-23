export default defineNuxtConfig({
  compatibilityDate: '2025-07-15',
  ssr: false, // SPA mode — no SSR
  devtools: { enabled: false },
  devServer: {
    port: 3000,
  },
  vite: {
    server: {
      proxy: {
        '/api': 'http://localhost:8080',
        '/ws': {
          target: 'http://localhost:8080',
          ws: true,
        },
      },
    },
  },
  app: {
    head: {
      title: 'Juli — Tmux Web Console',
      meta: [
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      ],
      // Per-machine name and icon, served by juli itself (see
      // internal/server/identity.go). The manifest is fetched with
      // credentials so it passes an auth proxy such as Cloudflare Access.
      link: [
        { rel: 'apple-touch-icon', href: '/apple-touch-icon.png' },
        { rel: 'manifest', href: '/manifest.webmanifest', crossorigin: 'use-credentials' },
      ],
    },
  },
})
