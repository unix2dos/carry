package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type localAPI struct {
	engine      *Engine
	host, token string
	ctx         context.Context
	workers     sync.WaitGroup
}
type projectView struct {
	Project     Project            `json:"project"`
	Observation *Observation       `json:"observation"`
	Checks      *ApplicationChecks `json:"checks"`
	Operations  []Operation        `json:"operations"`
	Running     bool               `json:"running"`
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func (a *localAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	if r.Host != a.host {
		http.Error(w, "host rejected", http.StatusForbidden)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != "GET" {
			http.Error(w, "method rejected", 405)
			return
		}
		if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/style.css" {
			http.NotFound(w, r)
			return
		}
		sub, _ := fs.Sub(webFiles, "web")
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
		return
	}
	origin := "http://" + a.host
	if supplied := r.Header.Get("Origin"); supplied != "" && supplied != origin {
		http.Error(w, "origin rejected", 403)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.token)) != 1 {
		http.Error(w, "local session authentication required", 401)
		return
	}
	if r.Method == "POST" {
		if r.Header.Get("Origin") != origin || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "JSON same-origin request required", 403)
			return
		}
		var empty struct{}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&empty); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			http.Error(w, "invalid request", 400)
			return
		}
	}
	if r.URL.Path == "/api/projects" && r.Method == "GET" {
		projects, err := a.engine.Store.projects()
		if err != nil {
			jsonResponse(w, 500, map[string]string{"error": "local project records could not be read"})
			return
		}
		views := []projectView{}
		for _, p := range projects {
			ops, err := a.engine.Store.operations(p.Name)
			if err != nil {
				jsonResponse(w, 500, map[string]string{"error": "operation records could not be read"})
				return
			}
			if len(ops) > 10 {
				ops = ops[:10]
			}
			views = append(views, projectView{Project: p, Observation: a.engine.Store.observation(p.Name), Checks: a.engine.Store.checks(p.Name), Operations: ops, Running: a.engine.Store.busy(p.Name)})
		}
		jsonResponse(w, 200, views)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
	if len(parts) != 2 || !slugPattern.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	p, err := a.engine.Store.project(parts[0])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	switch parts[1] {
	case "refresh":
		if r.Method != "POST" {
			http.Error(w, "method rejected", 405)
			return
		}
		o, err := a.engine.refresh(ctx, p)
		if err != nil {
			jsonResponse(w, 502, map[string]string{"error": err.Error()})
			return
		}
		jsonResponse(w, 200, o)
	case "check":
		if r.Method != "POST" {
			http.Error(w, "method rejected", 405)
			return
		}
		c, err := a.engine.check(ctx, p)
		if err != nil {
			jsonResponse(w, 500, map[string]string{"error": "check result could not be saved"})
			return
		}
		jsonResponse(w, 200, c)
	case "logs":
		if r.Method != "GET" {
			http.Error(w, "method rejected", 405)
			return
		}
		lines, err := a.engine.Providers.logs(ctx, p)
		if err != nil {
			jsonResponse(w, 502, map[string]string{"error": err.Error()})
			return
		}
		jsonResponse(w, 200, map[string]any{"lines": lines})
	case "publish":
		if r.Method != "POST" {
			http.Error(w, "method rejected", 405)
			return
		}
		op, unlock, err := a.engine.begin(p)
		if err != nil {
			jsonResponse(w, 409, map[string]string{"error": err.Error()})
			return
		}
		initial := *op
		a.workers.Add(1)
		go func() { defer a.workers.Done(); defer unlock(); a.engine.executePublish(a.ctx, p, op, true) }()
		jsonResponse(w, 202, initial)
	case "reconcile":
		if r.Method != "POST" {
			http.Error(w, "method rejected", 405)
			return
		}
		op, err := a.engine.reconcile(ctx, p, false)
		if err != nil {
			jsonResponse(w, 409, map[string]string{"error": err.Error()})
			return
		}
		jsonResponse(w, 200, op)
	default:
		http.NotFound(w, r)
	}
}
func serve(ctx context.Context, e *Engine, port int, openBrowser bool) error {
	if port < 0 || port > 65535 {
		return errors.New("invalid loopback port")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	var key [32]byte
	if _, err = rand.Read(key[:]); err != nil {
		listener.Close()
		return err
	}
	token := hex.EncodeToString(key[:])
	api := &localAPI{engine: e, host: listener.Addr().String(), token: token, ctx: ctx}
	server := &http.Server{Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	address := "http://" + api.host + "/#" + token
	output(map[string]string{"url": address, "scope": "loopback only; fragment is a private local session key"})
	if openBrowser {
		name := "open"
		if runtime.GOOS == "linux" {
			name = "xdg-open"
		}
		browser := exec.Command(name, address)
		if err := browser.Start(); err != nil {
			listener.Close()
			return fmt.Errorf("browser could not be opened: start again without --open")
		}
		go browser.Wait()
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(closeCtx)
		workersDone := make(chan struct{})
		go func() { api.workers.Wait(); close(workersDone) }()
		select {
		case <-workersDone:
		case <-closeCtx.Done():
		}
	}
	return nil
}
