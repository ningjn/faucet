package kingbase

import (
	"context"
	"strings"
	"testing"

	"github.com/faucetdb/faucet/internal/connector"
)

// TestDriverName verifies the connector identifies itself as "kingbase",
// the name gokb registers with database/sql.
func TestDriverName(t *testing.T) {
	c := New()
	if got := c.DriverName(); got != "kingbase" {
		t.Fatalf("DriverName() = %q, want %q", got, "kingbase")
	}
}

// TestDelegatesPostgresDialect verifies the PG-compatible dialect surface:
// numbered placeholders, quoted identifiers, RETURNING and upsert support.
func TestDelegatesPostgresDialect(t *testing.T) {
	c := New()
	if got := c.ParameterPlaceholder(3); got != "$3" {
		t.Errorf("ParameterPlaceholder(3) = %q, want %q", got, "$3")
	}
	if got := c.QuoteIdentifier(`a"b`); got != `"a""b"` {
		t.Errorf(`QuoteIdentifier(%q) = %q, want %q`, `a"b`, got, `"a""b"`)
	}
	if !c.SupportsReturning() {
		t.Error("SupportsReturning() = false, want true")
	}
	if !c.SupportsUpsert() {
		t.Error("SupportsUpsert() = false, want true")
	}
}

// TestBuildSelectDelegatesToPostgres verifies query building reuses the
// postgres dialect: schema-qualified FROM clause and parameterized LIMIT.
func TestBuildSelectDelegatesToPostgres(t *testing.T) {
	c := New()
	sql, args, err := c.BuildSelect(context.Background(), connector.SelectRequest{
		Table:      "users",
		Fields:     []string{"id", "name"},
		Filter:     "age > $1",
		FilterArgs: []interface{}{18},
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("BuildSelect() error = %v", err)
	}
	wantSQL := `SELECT "id", "name" FROM "public"."users" WHERE age > $1 LIMIT $2`
	if sql != wantSQL {
		t.Errorf("BuildSelect() sql = %q, want %q", sql, wantSQL)
	}
	if len(args) != 2 || args[0] != 18 || args[1] != 5 {
		t.Errorf("BuildSelect() args = %v, want [18 5]", args)
	}
}

// TestConnectUsesKingbaseDriver verifies Connect routes through the gokb
// driver (registered as "kingbase" via sql.Register). A connection refused
// error is expected; "unknown driver" would mean the wiring is wrong.
func TestConnectUsesKingbaseDriver(t *testing.T) {
	c := New()
	err := c.Connect(connector.ConnectionConfig{
		Driver: "kingbase",
		DSN:    "kingbase://faucet_test:faucet_test@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
	})
	if err == nil {
		c.Disconnect()
		t.Fatal("Connect to port 1 should fail")
	}
	if strings.Contains(err.Error(), "unknown driver") {
		t.Fatalf("Connect did not use the kingbase driver: %v", err)
	}
}

// TestNormalizeDSN verifies DSN normalization for out-of-the-box parity with
// the postgres connector: gokb defaults to sslmode=require (unlike pgx's
// prefer) and only URL-parses kingbase:// scheme prefixes.
func TestNormalizeDSN(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "postgres prefix rewritten",
			in:   "postgres://user:pass@host:54321/db",
			want: "kingbase://user:pass@host:54321/db?sslmode=disable",
		},
		{
			name: "url without sslmode gets disable appended",
			in:   "kingbase://user:pass@host:54321/db",
			want: "kingbase://user:pass@host:54321/db?sslmode=disable",
		},
		{
			name: "url with existing query gets disable appended",
			in:   "kingbase://user:pass@host:54321/db?connect_timeout=5",
			want: "kingbase://user:pass@host:54321/db?connect_timeout=5&sslmode=disable",
		},
		{
			name: "explicit sslmode preserved",
			in:   "kingbase://user:pass@host:54321/db?sslmode=verify-full",
			want: "kingbase://user:pass@host:54321/db?sslmode=verify-full",
		},
		{
			name: "keyword DSN without sslmode gets disable appended",
			in:   "user=test password=test host=localhost port=54321 dbname=test",
			want: "user=test password=test host=localhost port=54321 dbname=test sslmode=disable",
		},
		{
			name: "keyword DSN with sslmode preserved",
			in:   "user=test host=localhost sslmode=require",
			want: "user=test host=localhost sslmode=require",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeDSN(tc.in); got != tc.want {
				t.Errorf("normalizeDSN(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
