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
	"strconv"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
)

type AppConfig struct {
	AppName   string
	PortAddr  string
	SQLiteDSN string
}

type Heartbeat struct {
	ID     string    `json:"id"`
	Expiry time.Time `json:"expiry"`
	Label  string    `json:"label,omitempty"`
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
				Name:        "external-port",
				Usage:       "Port for external GET endpoints",
				EnvVars:     []string{"PORT_ADDR"},
				Destination: &cf.PortAddr,
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
            expiry DATETIME NOT NULL,
			label TEXT
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
		externalServer := &http.Server{
			Addr:    cf.PortAddr,
			Handler: externalRouter(),
		}
		go func() {
			<-groupCtx.Done()
			if err := externalServer.Shutdown(context.Background()); err != nil {
				log.Printf("failed to shutdown server: %v", err)
			} else {
				log.Println("server shutdown")
			}
		}()
		log.Printf("external server starting on %s\n", cf.PortAddr)
		if err := externalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %v", err)
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

func externalRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{id}", handleHeartbeat)
	return mux
}

func handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	hbID := r.PathValue("id")
	if hbID == "" {
		http.Error(w, "ID value is required", http.StatusBadRequest)
		return
	}

	queryParams := r.URL.Query()
	expiryParam := queryParams.Get("expiry")
	labelParam := queryParams.Get("label")

	shouldStoreHeartbeat := expiryParam != "" || labelParam != ""

	if shouldStoreHeartbeat {
		expirySeconds, err := strconv.Atoi(expiryParam)
		if err != nil {
			http.Error(w, "expiry query parameter must be a valid integer", http.StatusBadRequest)
		}

		expiryTime := time.Now().Add(time.Duration(expirySeconds) * time.Second)

		_, err = db.Exec(`
			INSERT OR REPLACE INTO heartbeats (id, expiry, label)
			VALUES (?, ?, ?);
		`, hbID, expiryTime.Format(time.RFC3339), labelParam)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to store heartbeat: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
		return

	} else {
		var expiryStr, label string
		err := db.QueryRow(`
			SELECT expiry, label FROM heartbeats WHERE id = ?
		`, hbID).Scan(&expiryStr, &label)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "heartbeat not found", http.StatusNotFound)
			} else {
				http.Error(w, fmt.Sprintf("failed to query heartbeat: %v", err), http.StatusInternalServerError)
			}
			return
		}

		expiry, err := time.Parse(time.RFC3339, expiryStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to parse expiry date: %v", err), http.StatusInternalServerError)
			return
		}

		if time.Now().After(expiry) {
			http.Error(w, "heartbeat expired", http.StatusNotFound)
			return
		}

		response := Heartbeat{
			ID:     hbID,
			Expiry: expiry,
			Label:  label,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
		}
	}
}
