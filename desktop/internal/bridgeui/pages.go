package bridgeui

import (
	"bytes"
	"embed"
	"html/template"
	"io"
	"net/http"
	"path"
	"time"
)

//go:embed *.html assets/*
var files embed.FS

var pages = template.Must(template.ParseFS(files, "*.html"))

//go:embed assets/bridge.css
var bridgeCSS string

func WriteBridge(w io.Writer, target, gateway, quitURL string, state any) error {
	if target == "" {
		target = "192.168.1.1"
	}
	if gateway == "" {
		gateway = "192.168.1.1"
	}
	data := struct {
		Target, Gateway, QuitURL string
		State                    any
	}{target, gateway, quitURL, state}
	return pages.ExecuteTemplate(w, "bridge.html", data)
}

func WriteError(w io.Writer, target, message string) error {
	return pages.ExecuteTemplate(w, "error.html", struct{ Target, Message string }{target, message})
}

func WriteQuit(w io.Writer, openerURL, token string) error {
	return pages.ExecuteTemplate(w, "quit.html", struct {
		OpenerURL, Token string
		CSS              template.CSS
	}{openerURL, token, template.CSS(bridgeCSS)})
}

func ServeAsset(w http.ResponseWriter, r *http.Request, name string) {
	var contentType string
	switch path.Ext(name) {
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".js":
		contentType = "text/javascript; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	data, err := files.ReadFile("assets/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
