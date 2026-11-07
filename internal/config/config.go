package config

import (
	"fmt"
	"os"
)

const defaultAddr = ":8080"

// Config is the process configuration read from the environment.
type Config struct {
	DatabaseURL string
	Addr        string
}

// FromEnv reads DATABASE_URL and ADDR.
func FromEnv() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	return Config{DatabaseURL: databaseURL, Addr: addr}, nil
}
