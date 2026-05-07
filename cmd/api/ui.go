package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed ui/index.html
var uiFiles embed.FS

func (app *application) uiHandler(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(uiFiles, "ui/index.html")
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (app *application) frontendHandler() http.Handler {
	return http.HandlerFunc(app.uiHandler)
}
