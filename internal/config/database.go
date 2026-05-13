package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	EnvDatabaseDSN     = "WACHT_DATABASE_DSN"
	EnvDatabaseDSNFile = "WACHT_DATABASE_DSN_FILE"
)

// ResolveDatabaseDSN returns the database DSN from the supported runtime
// configuration sources, ordered from most explicit to least explicit.
func ResolveDatabaseDSN() (string, error) {
	if dsn := strings.TrimSpace(os.Getenv(EnvDatabaseDSN)); dsn != "" {
		return dsn, nil
	}

	path := strings.TrimSpace(os.Getenv(EnvDatabaseDSNFile))
	if path == "" {
		return "", fmt.Errorf("database DSN is required; set %s or %s", EnvDatabaseDSN, EnvDatabaseDSNFile)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read database DSN file: %w", err)
	}
	dsn := strings.TrimSpace(string(data))
	if dsn == "" {
		return "", fmt.Errorf("database DSN file %q is empty", path)
	}
	return dsn, nil
}
