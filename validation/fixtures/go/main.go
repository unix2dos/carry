// Validation fixture, not a production application.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const schema = `CREATE TABLE IF NOT EXISTS validation_records (id bigint GENERATED ALWAYS AS IDENTITY UNIQUE, key text PRIMARY KEY, value text NOT NULL)`

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func main() {
	log.SetFlags(0)
	token := os.Getenv("VALIDATION_TOKEN")
	port, err := strconv.Atoi(env("PORT", "8080"))
	if err != nil || port < 1 || port > 65535 || len(token) < 32 {
		log.Fatal(`{"error":"invalid_configuration"}`)
	}
	dsn, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil || dsn.Hostname() == "" || (dsn.Scheme != "postgres" && dsn.Scheme != "postgresql") {
		log.Fatal(`{"error":"invalid_configuration"}`)
	}
	mode := env("DATABASE_SSLMODE", "verify-full")
	if mode != "disable" && mode != "verify-full" {
		log.Fatal(`{"error":"invalid_configuration"}`)
	}
	query := dsn.Query()
	for _, name := range []string{"ssl", "sslmode", "sslrootcert", "sslcert", "sslkey", "uselibpqcompat"} {
		query.Del(name)
	}
	query.Set("sslmode", mode)
	if mode != "disable" {
		query.Set("sslrootcert", env("DATABASE_CA_CERT", "/etc/ssl/certs/ca-certificates.crt"))
	}
	dsn.RawQuery = query.Encode()
	config, err := pgx.ParseConfig(dsn.String())
	if err != nil {
		log.Fatal(`{"error":"invalid_configuration"}`)
	}
	config.ConnectTimeout = 3 * time.Second
	// ponytail: one connection per request for a low-rate fixture; use a pool for production throughput.
	connect := func(ctx context.Context) (*pgx.Conn, error) { return pgx.ConnectConfig(ctx, config) }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	db, err := connect(ctx)
	if err != nil {
		log.Fatal(`{"error":"database_unavailable"}`)
	}
	_, err = db.Exec(ctx, schema)
	db.Close(ctx)
	cancel()
	if err != nil {
		log.Fatal(`{"error":"database_unavailable"}`)
	}
	version := env("APP_VERSION", "v1")
	respond := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail := func(status int, code string) { respond(w, status, map[string]string{"error": code}) }
		if r.URL.Path == "/healthz" && r.Method == "GET" {
			respond(w, 200, map[string]string{"status": "ok", "language": "go", "version": version})
			return
		}
		ready := r.URL.Path == "/readyz" && r.Method == "GET"
		if !ready {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
				fail(401, "unauthorized")
				return
			}
			if !strings.HasPrefix(r.URL.Path, "/records/") {
				fail(404, "not_found")
				return
			}
		}
		key := strings.TrimPrefix(r.URL.Path, "/records/")
		if !ready {
			valid := len(key) > 0 && len(key) <= 80
			for _, c := range key {
				valid = valid && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_')
			}
			if !valid {
				fail(400, "invalid_key")
				return
			}
			if r.Method != "GET" && r.Method != "PUT" && r.Method != "DELETE" {
				fail(405, "method_not_allowed")
				return
			}
		}
		var value string
		if r.Method == "PUT" {
			body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
			if len(body) > 4096 {
				fail(413, "value_too_large")
				return
			}
			if err != nil || !utf8.Valid(body) || len(body) == 0 || strings.ContainsRune(string(body), 0) {
				fail(400, "invalid_value")
				return
			}
			value = string(body)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		db, err := connect(ctx)
		if err != nil {
			fail(503, "database_unavailable")
			return
		}
		defer db.Close(context.Background())
		if ready {
			if err := db.Ping(ctx); err != nil {
				fail(503, "database_unavailable")
			} else {
				respond(w, 200, map[string]string{"status": "ready"})
			}
			return
		}
		switch r.Method {
		case "GET":
			err = db.QueryRow(ctx, "SELECT value FROM validation_records WHERE key=$1", key).Scan(&value)
			if err == pgx.ErrNoRows {
				fail(404, "not_found")
				return
			}
		case "PUT":
			_, err = db.Exec(ctx, "INSERT INTO validation_records(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value", key, value)
		case "DELETE":
			_, err = db.Exec(ctx, "DELETE FROM validation_records WHERE key=$1", key)
		}
		if err != nil {
			fail(503, "database_unavailable")
			return
		}
		if r.Method == "DELETE" {
			respond(w, 200, map[string]bool{"deleted": true})
		} else {
			respond(w, 200, map[string]string{"key": key, "value": value})
		}
	})
	server := &http.Server{Addr: fmt.Sprintf("0.0.0.0:%d", port), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	log.Printf(`{"event":"listening","language":"go","port":%d}`, port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(`{"error":"http_server_stopped"}`)
	}
}
