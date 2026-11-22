// Pulihkan sesi (GET /auth/me) dari token localStorage sebelum navigasi pertama, plus sinkron antar-tab.
export default defineNuxtPlugin(async () => {
  const auth = useAuth()
  await auth.restore()
  window.addEventListener('storage', (e) => {
    if (e.key !== 'toko_token') return
    if (!e.newValue) auth.user.value = null
    else void auth.restore()
  })
})
