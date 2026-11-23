// Package knowledge: chunking, indexing embedding, dan pencarian kebijakan toko (RAG) — TRD 04 §3.2, §7.
package knowledge

import (
	"strings"
	"unicode/utf8"
)

// MaxChunkChars: batas karakter (rune) per chunk.
const MaxChunkChars = 800

// Split memecah teks per paragraf (baris kosong) lalu menggabungkannya hingga ≤ MaxChunkChars per chunk, tanpa overlap.
// Paragraf yang lebih panjang dari batas dipotong per kalimat (lalu per kata) agar tidak melebihi batas. Deterministik.
func Split(content string) []string {
	var paras []string
	for _, p := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			paras = append(paras, p)
		}
	}
	var chunks []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			chunks = append(chunks, s)
		}
		cur.Reset()
	}
	for _, p := range paras {
		for _, piece := range splitLong(p) {
			if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+2+utf8.RuneCountInString(piece) > MaxChunkChars {
				flush()
			}
			if cur.Len() > 0 {
				cur.WriteString("\n\n")
			}
			cur.WriteString(piece)
		}
	}
	flush()
	return chunks
}

// splitLong memecah satu paragraf > MaxChunkChars menjadi potongan ≤ MaxChunkChars (per kalimat, fallback per kata).
func splitLong(p string) []string {
	if utf8.RuneCountInString(p) <= MaxChunkChars {
		return []string{p}
	}
	var out []string
	var cur strings.Builder
	emit := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, w := range strings.Fields(p) { // per kata menjaga batas; kalimat tetap utuh sebisanya karena kata berurutan
		if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+1+utf8.RuneCountInString(w) > MaxChunkChars {
			emit()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	emit()
	return out
}
