package middleware

import (
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Auth returns an HTTP middleware that enforces trusted-proxy header auth.
//
// Three modes (all env-driven, decided at startup):
//
//   (a) Open mode: both trustedCIDR and headerName are empty.
//       Middleware is a no-op — all requests pass through.
//       Intended for localhost / trusted LAN.
//
//   (b) Auth mode: headerName is set.
//       Determine the peer IP from r.RemoteAddr ONLY (never X-Forwarded-For).
//       Parse trustedCIDRs (comma-separated); empty CIDR list = trust nobody.
//       If peer is NOT in a trusted CIDR  → 403 Forbidden.
//       If peer IS trusted but headerName is missing/empty in request → 401 Unauthorized.
//       Otherwise → pass through (authenticated; identity not stored).
//
//   (c) Misconfiguration: trustedCIDR is set but headerName is empty.
//       Log a startup warning; treat as open mode (explicit, not silent).
//
// /healthz must be excluded from auth and is handled by the caller.
func Auth(trustedCIDR, headerName string) func(http.Handler) http.Handler {
	// parse CIDRs once at startup
	var prefixes []netip.Prefix
	for _, raw := range strings.Split(trustedCIDR, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		p, err := netip.ParsePrefix(raw)
		if err != nil {
			log.Printf("auth middleware: ignoring invalid CIDR %q: %v", raw, err)
			continue
		}

		prefixes = append(prefixes, p)
	}

	// case (c): CIDR set but no header — misconfiguration
	if trustedCIDR != "" && headerName == "" {
		log.Printf("auth middleware: GINVOICE_TRUSTED_PROXY_CIDR set but GINVOICE_AUTH_HEADER empty — auth inactive")
		// fall through to open mode
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// case (a): open mode
			if headerName == "" {
				next.ServeHTTP(w, r)
				return
			}

			// case (b): auth mode — check peer IP against trusted CIDRs
			peerIP, err := parseRemoteIP(r.RemoteAddr)
			if err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			if !isTrusted(peerIP, prefixes) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			if r.Header.Get(headerName) == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseRemoteIP(remoteAddr string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// try parsing directly (no port)
		host = remoteAddr
	}

	return netip.ParseAddr(host)
}

func isTrusted(ip netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}

	return false
}
