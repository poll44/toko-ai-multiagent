// Nuxt 3 SPA murni (ssr:false) — JWT di localStorage, browser memanggil gateway langsung. docs/prd/07-SERVICE-FRONTEND.md §7
export default defineNuxtConfig({
  ssr: false, // wajib: SPA, tanpa render server & tanpa server routes (Nitro API)
  compatibilityDate: '2024-11-01',
  devServer: { port: Number(process.env.FRONTEND_PORT ?? 3000) },
  runtimeConfig: {
    public: {
      apiBase: 'http://localhost:8080', // override: NUXT_PUBLIC_API_BASE (ikut ter-bundle ke client: JANGAN taruh rahasia)
    },
  },
  modules: ['@nuxtjs/tailwindcss'],
  app: {
    head: {
      title: 'Toko AI',
      meta: [{ name: 'viewport', content: 'width=device-width, initial-scale=1' }],
    },
  },
  typescript: { strict: true },
})
