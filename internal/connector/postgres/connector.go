package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/faucetdb/faucet/internal/connector"
)

// PostgresConnector implements connector.Connector for PostgreSQL databases.
type PostgresConnector struct {
	db         *sqlx.DB
	schemaName string
	driverName string // database/sql driver name used by sqlx.Connect
}

// New creates a new PostgresConnector with default settings.
func New() connector.Connector {
	return NewWithDriver("pgx")
}

// NewWithDriver creates a PostgresConnector that connects through the given
// database/sql driver name. PostgreSQL-compatible databases (e.g. KingbaseES)
// reuse this implementation with their own driver.
func NewWithDriver(driverName string) *PostgresConnector {
	return &PostgresConnector{schemaName: "public", driverName: driverName}
}

// Connect establishes a connection to the PostgreSQL database using the
// provided configuration. It configures connection pool settings and stores
// the schema name for introspection queries.
func (c *PostgresConnector) Connect(cfg connector.ConnectionConfig) error {
	db, err := sqlx.Connect(c.driverName, cfg.DSN)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}

	if cfg.SchemaName != "" {
		c.schemaName = cfg.SchemaName
	}

	c.db = db
	return nil
}

// BeginTx starts a new database transaction with the given options.
func (c *PostgresConnector) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sqlx.Tx, error) {
	return c.db.BeginTxx(ctx, opts)
}

// Disconnect closes the database connection pool.
func (c *PostgresConnector) Disconnect() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// Ping verifies the database connection is alive.
func (c *PostgresConnector) Ping(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

// DB returns the underlying sqlx.DB connection pool.
func (c *PostgresConnector) DB() *sqlx.DB {
	return c.db
}

// DriverName returns the driver identifier for PostgreSQL.
func (c *PostgresConnector) DriverName() string { return "postgres" }

// QuoteIdentifier wraps a SQL identifier in double quotes, escaping any
// embedded double quotes to prevent SQL injection.
func (c *PostgresConnector) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// SupportsReturning indicates that PostgreSQL supports RETURNING clauses.
func (c *PostgresConnector) SupportsReturning() bool { return true }

// SupportsUpsert indicates that PostgreSQL supports ON CONFLICT (upsert).
func (c *PostgresConnector) SupportsUpsert() bool { return true }

// ParameterPlaceholder returns a PostgreSQL-style numbered parameter
// placeholder (e.g., $1, $2, $3).
func (c *PostgresConnector) ParameterPlaceholder(index int) string {
	return fmt.Sprintf("$%d", index)
}
