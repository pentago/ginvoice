package middleware

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuth(t *testing.T) {
	tests := []struct {
		name       string
		cidr       string
		header     string  // env: header name to trust
		peerAddr   string  // simulated r.RemoteAddr
		reqHeader  string  // value sent in request header (empty = not sent)
		xff        string  // optional X-Forwarded-For spoof attempt
		wantStatus int
		wantLog    string // optional: check startup log message
	}{
		{
			name:       "1_open_mode_both_unset",
			cidr:       "", header: "", peerAddr: "1.2.3.4:0", reqHeader: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "2_trusted_peer_with_header",
			cidr:       "127.0.0.1/32", header: "Remote-User",
			peerAddr:   "127.0.0.1:0", reqHeader: "alice",
			wantStatus: http.StatusOK,
		},
		{
			name:       "3_trusted_peer_missing_header",
			cidr:       "127.0.0.1/32", header: "Remote-User",
			peerAddr:   "127.0.0.1:0", reqHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "4_untrusted_peer",
			cidr:       "127.0.0.1/32", header: "Remote-User",
			peerAddr:   "10.0.0.1:0", reqHeader: "alice",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "5_spoofed_remote_user_xforwardedfor_ignored",
			cidr:       "127.0.0.1/32", header: "Remote-User",
			peerAddr:   "10.0.0.1:0", reqHeader: "alice", // X-Forwarded-For would be set too
			xff:        "127.0.0.1",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "6_cidr_empty_header_set_nobody_trusted",
			cidr:       "", header: "Remote-User",
			peerAddr:   "127.0.0.1:0", reqHeader: "alice",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "7_cidr_set_header_env_empty_open_mode_with_warning",
			cidr:       "127.0.0.1/32", header: "",
			peerAddr:   "1.2.3.4:0", reqHeader: "",
			wantStatus: http.StatusOK, // open mode (misconfiguration warning logged)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			if tt.wantLog != "" {
				prevWriter := log.Writer()
				log.SetOutput(&logBuf)
				defer log.SetOutput(prevWriter)
			}

			handler := Auth(tt.cidr, tt.header)

			var called bool
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})
			wrapped := handler(next)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.peerAddr
			if tt.reqHeader != "" {
				req.Header.Set(tt.header, tt.reqHeader)
			}
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}

			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK && !called {
				t.Fatal("next handler not called on pass-through")
			}
			if tt.wantStatus != http.StatusOK && called {
				t.Fatal("next handler called on rejected request")
			}

			if tt.wantLog != "" && !strings.Contains(logBuf.String(), tt.wantLog) {
				t.Fatalf("log output %q does not contain %q", logBuf.String(), tt.wantLog)
			}
		})
	}
}
