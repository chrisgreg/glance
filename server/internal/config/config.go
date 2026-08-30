// Package config reads Glance's configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the full server configuration.
type Config struct {
	Port         int
	DatabasePath string
	LogLevel     string

	// Days of raw events to keep. Rollups are kept forever; raw rows only
	// feed today's and yesterday's rebuild, so two days is the floor.
	RetentionDays int
	// RetentionDaysSet is true when GLANCE_RETENTION_DAYS was given; the
	// value then overrides whatever was saved from the settings page.
	RetentionDaysSet bool

	// Admin login for the dashboard and admin API. Both must be set to enable it.
	AdminUser     string
	AdminPassword string

	// MCPToken, when set, is a bearer token that grants read-only access to
	// the MCP endpoint (/mcp). The admin login works there too.
	MCPToken string
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	c := Config{
		Port:          8080,
		DatabasePath:  env("GLANCE_DATABASE_PATH", "/data/glance.db"),
		LogLevel:      env("GLANCE_LOG_LEVEL", "info"),
		RetentionDays: 7,
		AdminUser:     env("GLANCE_ADMIN_USER", ""),
		AdminPassword: env("GLANCE_ADMIN_PASSWORD", ""),
		MCPToken:      strings.TrimSpace(env("GLANCE_MCP_TOKEN", "")),
	}
	if v := os.Getenv("GLANCE_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p <= 0 || p > 65535 {
			return c, fmt.Errorf("GLANCE_PORT must be a port number, got %q", v)
		}
		c.Port = p
	}
	if v := os.Getenv("GLANCE_RETENTION_DAYS"); v != "" {
		d, err := strconv.Atoi(v)
		if err != nil || d < 2 {
			return c, fmt.Errorf("GLANCE_RETENTION_DAYS must be an integer of at least 2, got %q", v)
		}
		c.RetentionDays = d
		c.RetentionDaysSet = true
	}
	if (c.AdminUser == "") != (c.AdminPassword == "") {
		return c, fmt.Errorf("GLANCE_ADMIN_USER and GLANCE_ADMIN_PASSWORD must be set together")
	}
	if c.AdminPassword != "" && len(c.AdminPassword) < 8 {
		return c, fmt.Errorf("GLANCE_ADMIN_PASSWORD must be at least 8 characters")
	}
	if c.MCPToken != "" && len(c.MCPToken) < 16 {
		return c, fmt.Errorf("GLANCE_MCP_TOKEN must be at least 16 characters")
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}
