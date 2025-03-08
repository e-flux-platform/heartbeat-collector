package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/urfave/cli/v2"
)

type AppConfig struct {
	EnvName      string
	AppName      string
	InternalAddr string
	ExternalAddr string
	SQLiteDSN    string
}

type Heartbeat struct {
	Secret   string            `json:"secret"`
	Expiry   time.Time         `json:"expiry"`
	Label    string            `json:"label,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
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
				Name:        "environment-name",
				Usage:       "Application Environment",
				EnvVars:     []string{"ENV_NAME"},
				Destination: &cf.EnvName,
			},
			&cli.StringFlag{
				Name:        "internal-port",
				Usage:       "Port for internal POST endpoints",
				EnvVars:     []string{"INTERNAL_ADDR"},
				Destination: &cf.InternalAddr,
			},
			&cli.StringFlag{
				Name:        "external-port",
				Usage:       "Port for external GET endpoints",
				EnvVars:     []string{"EXTERNAL_ADDR"},
				Destination: &cf.ExternalAddr,
			},
			&cli.StringFlag{
				Name:        "db-path",
				Usage:       "Path to the SQLite database file",
				EnvVars:     []string{"SQLITE_DSN"},
				Destination: &cf.SQLiteDSN,
			},
		},
		Action: run,
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(ctx *cli.Context) error {
	var err error
	db, err = sql.Open("sqlite3", cf.SQLiteDSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS heartbeats (
            secret TEXT PRIMARY KEY,
            expiry DATETIME NOT NULL,
            label TEXT,
            metadata TEXT
        );
    `)
	if err != nil {
		return fmt.Errorf("failed to create table: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		internalServer := &http.Server{
			Addr:    cf.InternalAddr,
			Handler: internalRouter(),
		}
		log.Printf("Internal server listening on %s\n", cf.InternalAddr)
		if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Internal server error: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		externalServer := &http.Server{
			Addr:    cf.ExternalAddr,
			Handler: externalRouter(),
		}
		log.Printf("External server listening on %s\n", cf.ExternalAddr)
		if err := externalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("External server error: %v", err)
		}
	}()

	wg.Wait()
	log.Println("All servers stopped")

	return nil
}

func internalRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /hb/{secret}", handlePutHeartbeat)
	return mux
}

func externalRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hb/{secret}", handleGetHeartbeat)
	return mux
}

func handlePutHeartbeat(w http.ResponseWriter, r *http.Request) {
	var hb Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if hb.Secret == "" {
		http.Error(w, "Secret value is required", http.StatusBadRequest)
		return
	}
	if hb.Expiry.IsZero() {
		http.Error(w, "Expiry date is required and must be RFC3339 compliant", http.StatusBadRequest)
		return
	}

	metadataJSON, err := json.Marshal(hb.Metadata)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode metadata: %v", err), http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(`
       INSERT OR REPLACE INTO heartbeats (secret, expiry, label, metadata)
        VALUES (?, ?, ?, ?);
    `, hb.Secret, hb.Expiry.Format(time.RFC3339), hb.Label, string(metadataJSON))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to store heartbeat: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("Heartbeat registered")); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}

func handleGetHeartbeat(w http.ResponseWriter, r *http.Request) {
	secret := r.PathValue("secret")
	if secret == "" {
		http.Error(w, "Secret value is required", http.StatusBadRequest)
		return
	}

	var expiryStr, label, metadataJSON string
	err := db.QueryRow(`
        SELECT expiry, label, metadata FROM heartbeats WHERE secret = ?
    `, secret).Scan(&expiryStr, &label, &metadataJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Heartbeat not found", http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Failed to query heartbeat: %v", err), http.StatusInternalServerError)
		}
		return
	}

	expiry, err := time.Parse(time.RFC3339, expiryStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse expiry date: %v", err), http.StatusInternalServerError)
		return
	}

	if time.Now().After(expiry) {
		http.Error(w, "Heartbeat expired", http.StatusNotFound)
		return
	}

	var metadata map[string]string
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		http.Error(w, fmt.Sprintf("Failed to decode metadata: %v", err), http.StatusInternalServerError)
		return
	}

	response := Heartbeat{
		Secret:   secret,
		Expiry:   expiry,
		Label:    label,
		Metadata: metadata,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
	}
}
