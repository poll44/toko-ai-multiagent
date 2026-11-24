// Package proxy: reverse proxy ke domain services memakai httputil.NewSingleHostReverseProxy (TRD 01 §8).
package proxy // nama package: proxy

import ( // package yang dipakai
	"context"           // context
	"errors"            // errors.Is / errors.As
	"net"               // dialer & net.Error
	"net/http"          // tipe HTTP
	"net/http/httputil" // reverse proxy bawaan Go
	"net/url"           // URL tujuan
	"strings"           // utilitas string
	"time"              // durasi

	shared "toko-ai-microservices/internal/shared/httputil" // helper HTTP bersama (alias shared)
)

// Options: konfigurasi satu reverse proxy.
type Options struct {
	Target             *url.URL      // alamat service tujuan
	InternalKey        string        // kunci internal yang disuntikkan
	Timeout            time.Duration // ResponseHeaderTimeout ke upstream
	StripPrefix        string        // mis. "/api/agent" (path publik -> path upstream)
	StripBrowserOrigin bool          // hapus Origin/Referer (khusus agent; ADK menolak request ber-Origin)
}

// New membangun proxy: menyuntik x-user-id, x-user-role, x-internal-api-key, x-request-id; menghapus Authorization
// (JWT TIDAK diteruskan ke service domain). Respons upstream diteruskan apa adanya; kegagalan jaringan => 502/504 JSON.
func New(o Options) http.Handler {
	rp := httputil.NewSingleHostReverseProxy(o.Target) // proxy dasar ke satu host
	base := rp.Director                                // Director bawaan
	rp.Director = func(r *http.Request) {              // Director kustom
		base(r)                  // set scheme/host & gabung path
		if o.StripPrefix != "" { // buang prefiks path publik
			r.URL.Path = strings.TrimPrefix(r.URL.Path, o.StripPrefix)
			if r.URL.Path == "" { // jangan kosong
				r.URL.Path = "/"
			}
			r.URL.RawPath = "" // paksa dihitung ulang dari Path
		}
		id := shared.IdentityFrom(r.Context())                // identitas hasil autentikasi
		r.Header.Set(shared.HeaderUserID, id.UserID)          // suntik user id
		r.Header.Set(shared.HeaderUserRole, id.Role)          // suntik role
		r.Header.Set(shared.HeaderInternalKey, o.InternalKey) // suntik kunci internal
		r.Header.Set(shared.HeaderRequestID, id.RequestID)    // teruskan request id
		r.Header.Del("Authorization")                         // JWT tidak diteruskan
		r.Header.Del(shared.HeaderServiceName)                // jaga-jaga; sudah disanitasi di depan
		if o.StripBrowserOrigin {                             // khusus agent
			r.Header.Del("Origin")
			r.Header.Del("Referer")
		}
		r.Host = o.Target.Host // Host header sesuai tujuan
	}
	rp.Transport = &http.Transport{ // transport kustom
		Proxy:                 nil,                                                 // jangan pakai proxy sistem
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext, // timeout koneksi 5 detik
		ResponseHeaderTimeout: o.Timeout,                                           // batas tunggu header respons
		MaxIdleConnsPerHost:   32,                                                  // koneksi idle per host
		IdleConnTimeout:       60 * time.Second,                                    // batas koneksi idle
	}
	rp.FlushInterval = -1                                                       // flush segera: wajib untuk SSE /run_sse
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { // tangani kegagalan upstream
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) ||
			strings.Contains(err.Error(), "timeout awaiting response headers") { // timeout
			shared.WriteError(w, r, http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT", "Service tujuan tidak merespons tepat waktu.", nil)
			return
		}
		if errors.Is(err, context.Canceled) { // klien memutus koneksi
			return
		}
		shared.WriteError(w, r, http.StatusBadGateway, "UPSTREAM_ERROR", "Service tujuan tidak dapat dihubungi.", nil) // gagal koneksi
	}
	return rp
}
