// Command server runs the Waymark HTTP service.
//
// Configuration (environment variables):
//
//	WAYMARK_ADMIN_PASSCODE owner secret for browser approval
//	WAYMARK_TOKEN_TTL_DAYS  (default 90, range 1..365)
//	DB_PATH                  SQLite file path        (default ./waymark.db)
//	PORT                     listen port             (default 8080)
//	PUBLIC_BASE_URL          content origin, e.g. https://pages.example.com
//	CONTROL_BASE_URL         control origin for API and browser authorization
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Onkarj012/Waymark/internal/store"
	"github.com/Onkarj012/Waymark/internal/web"
)

func main() {
	dbPath := getenv("DB_PATH", "./waymark.db")
	port := getenv("PORT", "8080")
	tokenTTLDays, err := parseIntEnv("WAYMARK_TOKEN_TTL_DAYS", 90)
	if err != nil {
		log.Fatal(err)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go sweepExpired(ctx, st, time.Hour)

	handler, err := web.NewConfigured(st, web.Config{
		AdminPasscode:    os.Getenv("WAYMARK_ADMIN_PASSCODE"),
		PublicBaseURL:    os.Getenv("PUBLIC_BASE_URL"),
		ControlBaseURL:   os.Getenv("CONTROL_BASE_URL"),
		TokenTTLDays:     tokenTTLDays,
		TrustForwardedIP: os.Getenv("RAILWAY_ENVIRONMENT_ID") != "",
	})
	if err != nil {
		log.Fatalf("configure server: %v", err)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("waymark listening on :%s (db=%s)", port, dbPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

// sweepExpired periodically deletes expired pages until the context is cancelled.
func sweepExpired(ctx context.Context, st *store.Store, every time.Duration) {
	sweep := func() {
		if n, err := st.DeleteExpired(time.Now()); err != nil {
			log.Printf("expiry sweep: %v", err)
		} else if n > 0 {
			log.Printf("expiry sweep: removed %d page(s)", n)
		}
		if n, err := st.DeleteExpiredAuth(time.Now()); err != nil {
			log.Printf("auth expiry sweep: %v", err)
		} else if n > 0 {
			log.Printf("auth expiry sweep: removed %d record(s)", n)
		}
	}
	sweep() // run once at startup
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseIntEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New(key + " must be an integer")
	}
	return parsed, nil
}
