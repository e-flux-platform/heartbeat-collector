package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
)

type AppConfig struct {
	AppName      string
	InternalAddr string
	ExternalAddr string
	SQLiteDSN    string
}

type Heartbeat struct {
	ID            string    `json:"id"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

var (
	cf = AppConfig{
		AppName: "heartbeat-collector",
	}
	db *sql.DB
)

func main() {
	app := &cli.App{
		Name:  cf.AppName,
		Usage: "A service to collect and monitor heartbeats",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "internal-addr",
				Usage:       "Port for internal POST endpoints",
				EnvVars:     []string{"INTERNAL_ADDR"},
				Destination: &cf.InternalAddr,
				Value:       ":8181",
			},
			&cli.StringFlag{
				Name:        "external-port",
				Usage:       "Port for external GET endpoints",
				EnvVars:     []string{"EXTERNAL_ADDR"},
				Destination: &cf.ExternalAddr,
				Value:       ":8080",
			},
			&cli.StringFlag{
				Name:        "db-path",
				Usage:       "Path to the SQLite database file",
				EnvVars:     []string{"SQLITE_DSN"},
				Destination: &cf.SQLiteDSN,
				Value:       "/tmp/heartbeats.db",
			},
		},
		Action: run,
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(cliCtx *cli.Context) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	var err error
	db, err = sql.Open("sqlite3", cf.SQLiteDSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer func() {
		_ = db.Close()
		log.Printf("closed DB at %s\n", cf.SQLiteDSN)
	}()

	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS heartbeats (
            id TEXT PRIMARY KEY,
            last_updated_at DATETIME NOT NULL
        );
    `)
	if err != nil {
		return fmt.Errorf("failed to create table: %v", err)
	}

	log.Printf("DB opened at %s\n", cf.SQLiteDSN)

	ctx, exitApp := context.WithCancel(cliCtx.Context)
	defer exitApp()

	g, groupCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		internalServer := &http.Server{
			Addr:    cf.InternalAddr,
			Handler: internalRouter(),
		}

		go func() {
			<-groupCtx.Done()
			if err := internalServer.Shutdown(context.Background()); err != nil {
				log.Printf("failed to shutdown internal server: %v", err)
			} else {
				log.Println("internal server shutdown")
			}

		}()

		log.Printf("internal server starting on %s\n", cf.InternalAddr)
		if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("internal server error: %v", err)
		}
		return nil
	})

	g.Go(func() error {
		externalServer := &http.Server{
			Addr:    cf.ExternalAddr,
			Handler: externalRouter(),
		}
		go func() {
			<-groupCtx.Done()
			if err := externalServer.Shutdown(context.Background()); err != nil {
				log.Printf("failed to shutdown external server: %v", err)
			} else {
				log.Println("external server shutdown")
			}
		}()
		log.Printf("external server starting on %s\n", cf.ExternalAddr)
		if err := externalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("external server error: %v", err)
		}
		return nil
	})

	g.Go(func() error {
		signalChannel := make(chan os.Signal, 1)
		signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

		log.Println("starting signal listener")

		select {
		case sig := <-signalChannel:
			log.Printf("received sig-%s, exiting\n", sig)
			exitApp()
		case <-groupCtx.Done():
			log.Println("ending signal listener, main context done")
			return groupCtx.Err()
		}

		return nil
	})

	err = g.Wait()
	return err
}

func internalRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/{id}", handlePutHeartbeat)
	return mux
}

func externalRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{id}", handleGetHeartbeat)
	return mux
}

func handlePutHeartbeat(w http.ResponseWriter, r *http.Request) {
	hbID := r.PathValue("id")
	if hbID == "" {
		http.Error(w, "ID value is required on path", http.StatusBadRequest)
		return
	}

	_, err := db.Exec(`
       INSERT OR REPLACE INTO heartbeats (id, last_updated_at)
        VALUES (?, ?);
    `, hbID, time.Now().Format(time.RFC3339))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to store heartbeat: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleGetHeartbeat(w http.ResponseWriter, r *http.Request) {
	hbID := r.PathValue("id")
	if hbID == "" {
		http.Error(w, "ID value is required", http.StatusBadRequest)
		return
	}

	ttl := r.URL.Query().Get("ttl")
	if ttl == "" {
		http.Error(w, "ttl query parameter is required", http.StatusBadRequest)
		return
	}

	ttlSeconds, err := time.ParseDuration(ttl)
	if err != nil {
		http.Error(w, "ttl query parameter must be a valid duration", http.StatusBadRequest)
		return
	}

	var lastUpdatedAtStr string
	err = db.QueryRow(`
        SELECT last_updated_at FROM heartbeats WHERE id = ?
    `, hbID).Scan(&lastUpdatedAtStr)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "heartbeat not found", http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("failed to query heartbeat: %v", err), http.StatusInternalServerError)
		}
		return
	}

	lastUpdatedAt, err := time.Parse(time.RFC3339, lastUpdatedAtStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to parse last updated at date: %v", err), http.StatusInternalServerError)
		return
	}

	expiryTime := lastUpdatedAt.Add(time.Duration(ttlSeconds) * time.Second)
	if time.Now().After(expiryTime) {
		http.Error(w, "heartbeat expired", http.StatusNotFound)
		return
	}

	response := Heartbeat{
		ID:            hbID,
		LastUpdatedAt: lastUpdatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
	}
}
