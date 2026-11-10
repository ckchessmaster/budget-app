package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
)

func main() {
	var dbURL string
	flag.StringVar(&dbURL, "database-url", "", "PostgreSQL database URL")
	flag.Parse()

	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}

	command := "up"
	args := flag.Args()
	if len(args) > 0 {
		command = args[0]
	}

	migrator, err := db.NewMigrator(dbURL)
	if err != nil {
		log.Fatalf("failed to create migrator: %v", err)
	}

	switch command {
	case "up":
		fmt.Println("Applying migrations (up)...")
		if err := migrator.Up(); err != nil {
			log.Fatalf("migration up failed: %v", err)
		}
		v, dirty, err := migrator.Version()
		if err != nil {
			log.Printf("applied migrations, but failed to read version: %v", err)
		} else {
			fmt.Printf("Migrations applied successfully. Current version: %d (dirty: %t)\n", v, dirty)
		}
	case "down":
		fmt.Println("Reverting migrations (down)...")
		if err := migrator.Down(); err != nil {
			log.Fatalf("migration down failed: %v", err)
		}
		v, dirty, err := migrator.Version()
		if err != nil {
			fmt.Printf("Reverted all migrations. (info: %v)\n", err)
		} else {
			fmt.Printf("Migrations reverted. Current version: %d (dirty: %t)\n", v, dirty)
		}
	case "version":
		v, dirty, err := migrator.Version()
		if err != nil {
			log.Fatalf("failed to get migration version: %v", err)
		}
		fmt.Printf("Current migration version: %d, dirty: %t\n", v, dirty)
	default:
		log.Fatalf("unknown migration command: %s. Expected 'up', 'down', or 'version'", command)
	}
}
