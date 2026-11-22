// Package config: helper env untuk semua service Go (tanpa dependensi eksternal).
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// LoadDotEnv membaca file .env (KEY=VALUE) secara berurutan. Variabel yang sudah ada di
// environment proses TIDAK ditimpa, dan file yang lebih awal menang atas yang belakangan.
// File yang tidak ada diabaikan. Contoh prioritas: env proses > service-x/.env > .env (root).
func LoadDotEnv(paths ...string) {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(k), "export "))
			v = strings.TrimSpace(v)
			if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
				v = v[1 : len(v)-1]
			} else if i := strings.Index(v, " #"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
		_ = f.Close()
	}
}

// String mengembalikan env atau default bila kosong.
func String(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Int mengembalikan env sebagai integer atau default (juga bila tidak valid).
func Int(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Float mengembalikan env sebagai float64 atau default.
func Float(key string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

// Duration membaca format Go ("8h", "30s"); default bila kosong/tidak valid.
func Duration(key string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Millis membaca env sebagai milidetik (mis. UPSTREAM_TIMEOUT_MS=10000).
func Millis(key string, defMS int) time.Duration {
	return time.Duration(Int(key, defMS)) * time.Millisecond
}

// Require memastikan semua variabel terisi (fail-fast saat start).
func Require(keys ...string) error {
	var missing []string
	for _, k := range keys {
		if strings.TrimSpace(os.Getenv(k)) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("variabel environment wajib belum diisi: %s", strings.Join(missing, ", "))
	}
	return nil
}
