// Format uang Rp & waktu lokal Asia/Jakarta (PRD 07 §1.5).
export const idr = (n: number | null | undefined) => `Rp${new Intl.NumberFormat('id-ID').format(n ?? 0)}`

export const fmtDate = (iso: string | null | undefined) =>
  iso ? new Intl.DateTimeFormat('id-ID', { dateStyle: 'medium', timeStyle: 'short', timeZone: 'Asia/Jakarta' }).format(new Date(iso)) : '-'
