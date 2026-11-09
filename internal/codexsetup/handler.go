// Package codexsetup is the independent provider preview, not ordinary Magpie GUI.
package codexsetup

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/nativepreview"
)

//go:embed assets/*
var assets embed.FS

type Controller interface {
	State() nativepreview.State
	SelectExecutable(string) error
	AutoSelectExecutable(context.Context) error
	ConfigureAIHub(string, string) error
	Discover(context.Context, string) ([]nativepreview.Model, error)
	Login(context.Context) error
	Send(context.Context, string, string, string) (nativepreview.Reply, error)
	Cancel()
	Close() error
}

type Handler struct {
	controller Controller
	choose     func() (string, error)
	token      string
}

func NewHandler(controller Controller, choose func() (string, error)) (*Handler, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	return &Handler{controller: controller, choose: choose, token: hex.EncodeToString(token[:])}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; object-src 'none'; frame-src 'none'; frame-ancestors 'none'; form-action 'none'; base-uri 'none'")
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		name, mime := "", ""
		switch r.URL.Path {
		case "/":
			name, mime = "index.html", "text/html; charset=utf-8"
		case "/app.js":
			name, mime = "app.js", "text/javascript; charset=utf-8"
		case "/style.css":
			name, mime = "style.css", "text/css; charset=utf-8"
		default:
			http.NotFound(w, r)
			return
		}
		data, err := assets.ReadFile("assets/" + name)
		if err != nil {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		if name == "index.html" {
			data = []byte(strings.ReplaceAll(string(data), "__PREVIEW_TOKEN__", h.token))
		}
		w.Header().Set("Content-Type", mime)
		_, _ = w.Write(data)
		return
	}
	if r.Method != http.MethodPost || r.Header.Get("X-Preview-Token") != h.token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Fixed production Windows Wails beta.24 origin. Requests without Origin
	// still require the private per-window token; this handler is not a TCP service.
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://wails.localhost" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var in struct {
		Endpoint string `json:"endpoint"`
		Key      string `json:"key"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Text     string `json:"text"`
		Confirm  bool   `json:"confirm"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	write := func(value any) { _ = json.NewEncoder(w).Encode(value) }
	fail := func() {
		w.WriteHeader(http.StatusBadRequest)
		write(map[string]string{"error": "operation_unavailable"})
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	switch r.URL.Path {
	case "/api/state":
		write(h.controller.State())
	case "/api/auto":
		if h.controller.AutoSelectExecutable(ctx) != nil {
			fail()
			return
		}
		write(h.controller.State())
	case "/api/choose":
		path, err := h.choose()
		if err != nil {
			fail()
			return
		}
		if path == "" {
			write(map[string]bool{"cancelled": true})
			return
		}
		if h.controller.SelectExecutable(path) != nil {
			fail()
			return
		}
		write(map[string]string{"name": filepath.Base(path)})
	case "/api/configure":
		if !in.Confirm || h.controller.ConfigureAIHub(in.Endpoint, in.Key) != nil {
			fail()
			return
		}
		write(h.controller.State())
	case "/api/discover":
		if !in.Confirm || !h.controller.State().CanDiscover {
			fail()
			return
		}
		models, err := h.controller.Discover(ctx, in.Provider)
		if err != nil {
			fail()
			return
		}
		write(map[string]any{"models": models})
	case "/api/login":
		if !in.Confirm || !h.controller.State().CanLogin {
			fail()
			return
		}
		loginCtx, stop := context.WithTimeout(r.Context(), 10*time.Minute)
		defer stop()
		if h.controller.Login(loginCtx) != nil {
			fail()
			return
		}
		write(h.controller.State())
	case "/api/send":
		if !in.Confirm || !h.controller.State().CanSend {
			fail()
			return
		}
		reply, err := h.controller.Send(ctx, in.Provider, in.Model, in.Text)
		if err != nil {
			fail()
			return
		}
		write(reply)
	case "/api/cancel":
		h.controller.Cancel()
		write(map[string]bool{"cancelRequested": true})
	default:
		http.NotFound(w, r)
	}
}
