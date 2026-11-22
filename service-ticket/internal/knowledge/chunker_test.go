package knowledge

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitSmallDocIsOneChunk(t *testing.T) {
	got := Split("Paragraf satu.\n\nParagraf dua.")
	if len(got) != 1 || got[0] != "Paragraf satu.\n\nParagraf dua." {
		t.Fatalf("got %q", got)
	}
}

func TestSplitRespectsLimitAndKeepsAllText(t *testing.T) {
	para := strings.Repeat("kalimat uji kebijakan toko. ", 20) // ~540 karakter
	doc := para + "\n\n" + para + "\n\n" + para
	chunks := Split(doc)
	if len(chunks) < 2 {
		t.Fatalf("harus terpecah: %d", len(chunks))
	}
	var joined []string
	for _, c := range chunks {
		if n := utf8.RuneCountInString(c); n > MaxChunkChars {
			t.Fatalf("chunk %d karakter > %d", n, MaxChunkChars)
		}
		joined = append(joined, strings.Fields(c)...)
	}
	if want := strings.Fields(doc); len(joined) != len(want) {
		t.Fatalf("kata hilang/bertambah: %d vs %d", len(joined), len(want))
	}
}

func TestSplitLongParagraph(t *testing.T) {
	long := strings.Repeat("kata ", 600) // 3000 karakter tanpa baris kosong
	for _, c := range Split(long) {
		if utf8.RuneCountInString(c) > MaxChunkChars {
			t.Fatalf("chunk melebihi batas: %d", utf8.RuneCountInString(c))
		}
	}
}

func TestSplitDeterministicAndEmpty(t *testing.T) {
	if Split("   \n\n  ") != nil {
		t.Fatal("teks kosong => nil")
	}
	a, b := Split(seedDocs[4].Paras[0]+"\n\n"+seedDocs[4].Paras[1]), Split(seedDocs[4].Paras[0]+"\n\n"+seedDocs[4].Paras[1])
	if strings.Join(a, "|") != strings.Join(b, "|") {
		t.Fatal("harus deterministik")
	}
}

func TestSeedDocsAreSubstantive(t *testing.T) {
	if len(seedDocs) != 8 {
		t.Fatalf("harus 8 dokumen seed: %d", len(seedDocs))
	}
	for _, d := range seedDocs {
		content := strings.Join(d.Paras, "\n\n")
		if utf8.RuneCountInString(content) < 250 && d.Title != "Jam Layanan dan Kontak" && d.Title != "Pembatalan Pesanan" && d.Title != "Kebijakan Refund" {
			t.Errorf("%s terlalu pendek: %d", d.Title, utf8.RuneCountInString(content))
		}
		if len(Split(content)) < 1 {
			t.Errorf("%s: tidak menghasilkan chunk", d.Title)
		}
	}
}
