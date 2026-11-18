package main

import (
	"crypto/rand"
	"crypto/subtle"
	"net/http"

	"github.com/alaa/legacyrouteropener/internal/bridgeui"
)

const quitPath = "/quit"

type quitHandler struct {
	host, openerURL, token string
	stop                   func()
}

func newQuitHandler(host, openerURL string, stop func()) *quitHandler {
	return &quitHandler{host: host, openerURL: openerURL, token: rand.Text(), stop: stop}
}

func (q *quitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	if r.Host != q.host || r.URL.Path != quitPath {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = bridgeui.WriteQuit(w, q.openerURL, q.token)
	case http.MethodPost:
		if r.Header.Get("Origin") != "http://"+q.host {
			http.Error(w, "Open Quit app from the opener and confirm there.", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if err := r.ParseForm(); err != nil ||
			subtle.ConstantTimeCompare([]byte(r.PostForm.Get("token")), []byte(q.token)) != 1 {
			http.Error(w, "This confirmation is invalid. Return to the opener and try again.", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := bridgeui.WriteQuit(w, q.openerURL, ""); err == nil {
			q.stop()
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
