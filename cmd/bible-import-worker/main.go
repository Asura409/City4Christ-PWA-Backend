package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/bible"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	if _, err := pgx.ParseConfig(dsn); err != nil {
		log.Fatalf("DATABASE_URL is invalid: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		log.Fatalf("migrations failed: %v", err)
	}
	if err := bible.NewKJVImporter(db, nil).ImportKJV(ctx); err != nil {
		if errors.Is(err, bible.ErrRateLimited) {
			log.Fatal("HelloAO rate limited the KJV import; rerun the worker to resume")
		}
		log.Fatal(err)
	}
	log.Println("KJV import complete")
}
