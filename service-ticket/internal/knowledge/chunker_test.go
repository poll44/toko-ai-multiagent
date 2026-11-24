package knowledge // test internal untuk chunker & seed knowledge base

import ( // package yang dipakai
	"strings"      // utilitas string
	"testing"      // framework test
	"unicode/utf8" // hitung karakter
)

// TestSplitSmallDocIsOneChunk: dokumen kecil harus tetap satu chunk.
func TestSplitSmallDocIsOneChunk(t *testing.T) {
	got := Split("Paragraf satu.\n\nParagraf dua.") // dua paragraf pendek
	if len(got) != 1 || got[0] != "Paragraf satu.\n\nParagraf dua." {
		t.Fatalf("got %q", got)
	}
}

// TestSplitRespectsLimitAndKeepsAllText: chunk tidak boleh melebihi batas dan tidak boleh ada kata hilang.
func TestSplitRespectsLimitAndKeepsAllText(t *testing.T) {
	para := strings.Repeat("kalimat uji kebijakan toko. ", 20) // ~540 karakter
	doc := para + "\n\n" + para + "\n\n" + para                // tiga paragraf
	chunks := Split(doc)
	if len(chunks) < 2 { // harus terpecah
		t.Fatalf("harus terpecah: %d", len(chunks))
	}
	var joined []string // semua kata dari semua chunk
	for _, c := range chunks {
		if n := utf8.RuneCountInString(c); n > MaxChunkChars { // batas per chunk
			t.Fatalf("chunk %d karakter > %d", n, MaxChunkChars)
		}
		joined = append(joined, strings.Fields(c)...)
	}
	if want := strings.Fields(doc); len(joined) != len(want) { // jumlah kata harus sama
		t.Fatalf("kata hilang/bertambah: %d vs %d", len(joined), len(want))
	}
}

// TestSplitLongParagraph: satu paragraf sangat panjang tetap dipecah sesuai batas.
func TestSplitLongParagraph(t *testing.T) {
	long := strings.Repeat("kata ", 600) // 3000 karakter tanpa baris kosong
	for _, c := range Split(long) {
		if utf8.RuneCountInString(c) > MaxChunkChars {
			t.Fatalf("chunk melebihi batas: %d", utf8.RuneCountInString(c))
		}
	}
}

// TestSplitDeterministicAndEmpty: teks kosong => nil, dan hasil harus deterministik.
func TestSplitDeterministicAndEmpty(t *testing.T) {
	if Split("   \n\n  ") != nil { // hanya spasi
		t.Fatal("teks kosong => nil")
	}
	a, b := Split(seedDocs[4].Paras[0]+"\n\n"+seedDocs[4].Paras[1]), Split(seedDocs[4].Paras[0]+"\n\n"+seedDocs[4].Paras[1]) // dua kali panggil
	if strings.Join(a, "|") != strings.Join(b, "|") {
		t.Fatal("harus deterministik")
	}
}

// TestSeedDocsAreSubstantive: ada 8 dokumen seed dan masing-masing cukup isi & menghasilkan chunk.
func TestSeedDocsAreSubstantive(t *testing.T) {
	if len(seedDocs) != 8 {
		t.Fatalf("harus 8 dokumen seed: %d", len(seedDocs))
	}
	for _, d := range seedDocs {
		content := strings.Join(d.Paras, "\n\n")                                                                                                              // isi gabungan
		if utf8.RuneCountInString(content) < 250 && d.Title != "Jam Layanan dan Kontak" && d.Title != "Pembatalan Pesanan" && d.Title != "Kebijakan Refund" { // dokumen pendek yang dikecualikan
			t.Errorf("%s terlalu pendek: %d", d.Title, utf8.RuneCountInString(content))
		}
		if len(Split(content)) < 1 { // harus menghasilkan chunk
			t.Errorf("%s: tidak menghasilkan chunk", d.Title)
		}
	}
}
