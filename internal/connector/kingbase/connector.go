// Package kingbase implements connector.Connector for KingbaseES (人大金仓)
// databases, using the official gokb driver (vendored under third_party/gokb).
//
// KingbaseES V8R6 is wire-compatible with PostgreSQL 12, so introspection and
// query building delegate entirely to the postgres connector; only the
// database/sql driver name and the faucet-level driver identity differ.
package kingbase

import (
	"strings"

	_ "kingbase.com/gokb" // registers the "kingbase" database/sql driver

	"github.com/faucetdb/faucet/internal/connector"
	"github.com/faucetdb/faucet/internal/connector/postgres"
)

// KingbaseConnector implements connector.Connector for KingbaseES databases
// by embedding the postgres connector. Default port is 54321.
type KingbaseConnector struct {
	*postgres.PostgresConnector
}

// New creates a new KingbaseConnector. It reuses the postgres connector with
// the gokb driver ("kingbase" as registered with database/sql).
func New() connector.Connector {
	return &KingbaseConnector{
		PostgresConnector: postgres.NewWithDriver("kingbase"),
	}
}

// DriverName returns the faucet-level driver identifier for KingbaseES.
func (c *KingbaseConnector) DriverName() string { return "kingbase" }

// Connect establishes a connection after normalizing the DSN, then delegates
// pool setup to the embedded postgres connector.
func (c *KingbaseConnector) Connect(cfg connector.ConnectionConfig) error {
	normalized := cfg
	normalized.DSN = normalizeDSN(cfg.DSN)
	return c.PostgresConnector.Connect(normalized)
}

// normalizeDSN rewrites a KingbaseES DSN for out-of-the-box parity with the
// postgres connector:
//
//   - a postgres:// scheme prefix is rewritten to kingbase:// (gokb only
//     URL-parses the kingbase:// scheme; PG-style DSNs are otherwise treated
//     as keyword strings and fail confusingly)
//   - when no sslmode is present, sslmode=disable is appended: gokb defaults
//     to sslmode=require (unlike pgx's prefer), so unencrypted KingbaseES
//     installs would reject connections unless the DSN spells it out
func normalizeDSN(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") {
		dsn = "kingbase://" + strings.TrimPrefix(dsn, "postgres://")
	}
	if strings.Contains(dsn, "sslmode=") {
		return dsn
	}
	if strings.Contains(dsn, "://") {
		if strings.Contains(dsn, "?") {
			return dsn + "&sslmode=disable"
		}
		return dsn + "?sslmode=disable"
	}
	return dsn + " sslmode=disable"
}
