// Package knowledge: chunking, indexing embedding, dan pencarian kebijakan toko (RAG) — TRD 04 §3.2, §7.
package knowledge // nama package: knowledge

import ( // package yang dipakai
	"strings"      // utilitas string
	"unicode/utf8" // hitung karakter (rune)
)

// MaxChunkChars: batas karakter (rune) per chunk.
const MaxChunkChars = 800

// Split memecah teks per paragraf (baris kosong) lalu menggabungkannya hingga ≤ MaxChunkChars per chunk, tanpa overlap.
// Paragraf yang lebih panjang dari batas dipotong per kalimat (lalu per kata) agar tidak melebihi batas. Deterministik.
func Split(content string) []string {
	var paras []string                                                                   // daftar paragraf
	for _, p := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n\n") { // pecah di baris kosong
		if p = strings.TrimSpace(p); p != "" { // buang paragraf kosong
			paras = append(paras, p)
		}
	}
	var chunks []string     // hasil chunk
	var cur strings.Builder // chunk yang sedang disusun
	flush := func() {       // simpan chunk saat ini lalu kosongkan
		if s := strings.TrimSpace(cur.String()); s != "" {
			chunks = append(chunks, s)
		}
		cur.Reset()
	}
	for _, p := range paras { // gabungkan paragraf menjadi chunk
		for _, piece := range splitLong(p) { // paragraf panjang dipecah dulu
			if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+2+utf8.RuneCountInString(piece) > MaxChunkChars { // akan melebihi batas
				flush()
			}
			if cur.Len() > 0 { // pisahkan paragraf dengan baris kosong
				cur.WriteString("\n\n")
			}
			cur.WriteString(piece)
		}
	}
	flush() // simpan sisa terakhir
	return chunks
}

// splitLong memecah satu paragraf > MaxChunkChars menjadi potongan ≤ MaxChunkChars (per kalimat, fallback per kata).
func splitLong(p string) []string {
	if utf8.RuneCountInString(p) <= MaxChunkChars { // sudah cukup pendek
		return []string{p}
	}
	var out []string        // hasil potongan
	var cur strings.Builder // potongan yang sedang disusun
	emit := func() {        // simpan potongan saat ini lalu kosongkan
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, w := range strings.Fields(p) { // per kata menjaga batas; kalimat tetap utuh sebisanya karena kata berurutan
		if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+1+utf8.RuneCountInString(w) > MaxChunkChars { // akan melebihi batas
			emit()
		}
		if cur.Len() > 0 { // pisahkan kata dengan spasi
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	emit() // simpan sisa terakhir
	return out
}
