package main

import (
	"crypto/rand"
	"crypto/subtle"
	"io"
	"net/http"
	"time"

	"github.com/alaa/legacyrouteropener/internal/bridgeui"
)

const quitPath = "/quit"

type quitHandler struct {
	host, openerURL, token string
	stop                   func()
	watchTab               func() func()
	stopping               <-chan struct{}
}

func newQuitHandler(host, openerURL string, stop func()) *quitHandler {
	return &quitHandler{host: host, openerURL: openerURL, token: rand.Text(), stop: stop}
}

func (q *quitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	if r.Host != q.host {
		http.NotFound(w, r)
		return
	}

	switch r.URL.Path {
	case quitPath + "/events":
		q.watchConfirmation(w, r)
		return
	case quitPath + "/watch.js":
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		bridgeui.ServeAsset(w, r, "quit-watch.js")
		return
	case quitPath:
	default:
		http.NotFound(w, r)
		return
	}

	language := bridgeui.SelectLanguage(r.URL.Query().Get("lang"), r.Header.Get("Accept-Language"))
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = bridgeui.WriteQuit(w, language, q.openerURL, q.token)
	case http.MethodPost:
		if r.Header.Get("Origin") != "http://"+q.host {
			http.Error(w, bridgeui.Text(language, "quit_wrong_origin"), http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if err := r.ParseForm(); err != nil ||
			subtle.ConstantTimeCompare([]byte(r.PostForm.Get("token")), []byte(q.token)) != 1 {
			http.Error(w, bridgeui.Text(language, "quit_invalid_token"), http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := bridgeui.WriteQuit(w, language, q.openerURL, ""); err == nil {
			q.stop()
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, bridgeui.Text(language, "quit_wrong_method"), http.StatusMethodNotAllowed)
	}
}

func (q *quitHandler) watchConfirmation(w http.ResponseWriter, r *http.Request) {
	if q.watchTab == nil || r.Method != http.MethodGet || r.Header.Get("Sec-Fetch-Site") != "same-origin" {
		http.NotFound(w, r)
		return
	}
	stream := http.NewResponseController(w)
	if err := stream.SetWriteDeadline(time.Time{}); err != nil {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	endTab := q.watchTab()
	defer endTab()

	w.Header().Set("Content-Type", "text/event-stream")
	if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
		return
	}
	if err := stream.Flush(); err != nil {
		return
	}
	select {
	case <-r.Context().Done():
	case <-q.stopping:
	}
}
