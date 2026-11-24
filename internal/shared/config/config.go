// Package config: helper env untuk semua service Go (tanpa dependensi eksternal).
package config // nama package: config

import ( // daftar package yang dipakai
	"bufio"   // membaca file baris demi baris
	"fmt"     // memformat pesan error
	"os"      // akses file & environment proses
	"strconv" // konversi string ke angka
	"strings" // utilitas manipulasi string
	"time"    // tipe durasi
)

// LoadDotEnv membaca file .env (KEY=VALUE) secara berurutan. Variabel yang sudah ada di
// environment proses TIDAK ditimpa, dan file yang lebih awal menang atas yang belakangan.
// File yang tidak ada diabaikan. Contoh prioritas: env proses > service-x/.env > .env (root).
func LoadDotEnv(paths ...string) { // menerima satu atau lebih path file .env
	for _, p := range paths { // proses tiap file sesuai urutan prioritas
		f, err := os.Open(p) // buka file .env
		if err != nil {      // file tidak ada / tidak bisa dibuka
			continue // abaikan, lanjut ke file berikutnya
		}
		sc := bufio.NewScanner(f) // scanner untuk membaca per baris
		for sc.Scan() {           // ulangi sampai baris habis
			line := strings.TrimSpace(sc.Text())            // ambil baris & buang spasi pinggir
			if line == "" || strings.HasPrefix(line, "#") { // lewati baris kosong & komentar
				continue
			}
			k, v, ok := strings.Cut(line, "=") // pisahkan KEY dan VALUE pada "=" pertama
			if !ok {                           // tidak ada tanda "="
				continue // baris tidak valid, lewati
			}
			k = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(k), "export "))                         // bersihkan key & buang awalan "export "
			v = strings.TrimSpace(v)                                                                           // bersihkan spasi pada value
			if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) { // value diapit tanda kutip
				v = v[1 : len(v)-1] // buang kutip pembuka & penutup
			} else if i := strings.Index(v, " #"); i >= 0 { // value tanpa kutip yang punya komentar inline
				v = strings.TrimSpace(v[:i]) // potong bagian komentar
			}
			if _, exists := os.LookupEnv(k); !exists { // hanya set bila belum ada di environment
				_ = os.Setenv(k, v) // set variabel environment (error diabaikan)
			}
		}
		_ = f.Close() // tutup file setelah selesai dibaca
	}
}

// String mengembalikan env atau default bila kosong.
func String(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" { // ambil env & pastikan tidak kosong
		return v // pakai nilai dari env
	}
	return def // fallback ke default
}

// Int mengembalikan env sebagai integer atau default (juga bila tidak valid).
func Int(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" { // env terisi
		if n, err := strconv.Atoi(v); err == nil { // konversi ke int berhasil
			return n // pakai angka hasil konversi
		}
	}
	return def // kosong atau tidak valid -> default
}

// Float mengembalikan env sebagai float64 atau default.
func Float(key string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" { // env terisi
		if n, err := strconv.ParseFloat(v, 64); err == nil { // konversi ke float64 berhasil
			return n // pakai angka hasil konversi
		}
	}
	return def // kosong atau tidak valid -> default
}

// Duration membaca format Go ("8h", "30s"); default bila kosong/tidak valid.
func Duration(key string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" { // env terisi
		if d, err := time.ParseDuration(v); err == nil { // parse durasi berhasil
			return d // pakai durasi hasil parse
		}
	}
	return def // kosong atau tidak valid -> default
}

// Millis membaca env sebagai milidetik (mis. UPSTREAM_TIMEOUT_MS=10000).
func Millis(key string, defMS int) time.Duration {
	return time.Duration(Int(key, defMS)) * time.Millisecond // ubah angka ms menjadi time.Duration
}

// Require memastikan semua variabel terisi (fail-fast saat start).
func Require(keys ...string) error {
	var missing []string     // penampung nama variabel yang kosong
	for _, k := range keys { // cek tiap variabel wajib
		if strings.TrimSpace(os.Getenv(k)) == "" { // variabel kosong/tidak ada
			missing = append(missing, k) // catat sebagai hilang
		}
	}
	if len(missing) > 0 { // ada yang hilang
		return fmt.Errorf("variabel environment wajib belum diisi: %s", strings.Join(missing, ", ")) // kembalikan error berisi daftar
	}
	return nil // semua terisi
}
