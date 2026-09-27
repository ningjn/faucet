package kingbase

// Advanced integration coverage for the gokb driver path. These cases target
// the compatibility edges that a pgx-direct setup cannot be trusted with
// (DDL defaults, wide types, transactions, upserts, stored procedures), so
// regressions surface here against a real KingbaseES V8R6 instance.
//
// Run with (docker container from integration_test.go docs):
//
//	FAUCET_INTEGRATION=1 go test -count=1 -v ./internal/connector/kingbase/
//
// Skipped unless FAUCET_INTEGRATION is set (CI has no database).

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/faucetdb/faucet/internal/connector"
	"github.com/faucetdb/faucet/internal/model"
)

func kingbaseTestDSN() string {
	if dsn := os.Getenv("KINGBASE_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "kingbase://system:faucet123@127.0.0.1:54321/test?sslmode=disable"
}

func connectForIntegration(t *testing.T) connector.Connector {
	t.Helper()
	if os.Getenv("FAUCET_INTEGRATION") == "" {
		t.Skip("skipping kingbase integration tests: set FAUCET_INTEGRATION=1 to run")
	}
	conn := New()
	cfg := connector.ConnectionConfig{Driver: "kingbase", DSN: kingbaseTestDSN()}
	if err := conn.Connect(cfg); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { conn.Disconnect() })
	return conn
}

// TestKingbaseDDLWithDefaults covers CreateTable/IntrospectTable/DropTable
// through gokb, including DEFAULT expressions. faucet's Column.Default is a
// SQL literal by contract (introspection reads column_default verbatim), so
// string defaults must arrive quoted.
func TestKingbaseDDLWithDefaults(t *testing.T) {
	conn := connectForIntegration(t)
	ctx := context.Background()

	strDefault := "'hello'"
	intDefault := "42"
	err := conn.CreateTable(ctx, model.TableSchema{
		Name:       "kb_ddl_probe",
		Type:       "table",
		Columns: []model.Column{
			{Name: "id", Type: "serial", IsPrimaryKey: true, IsAutoIncrement: true},
			{Name: "note", Type: "text", Nullable: true, Default: &strDefault},
			{Name: "hits", Type: "int", Nullable: true, Default: &intDefault},
		},
		PrimaryKey: []string{"id"},
	})
	if err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}
	t.Cleanup(func() { conn.DropTable(ctx, "kb_ddl_probe") })

	// DDL default must be honored: insert without note/hits and read back.
	var note sql.NullString
	var hits sql.NullInt64
	if err := conn.DB().QueryRowxContext(ctx,
		`INSERT INTO kb_ddl_probe (id) VALUES (DEFAULT) RETURNING note, hits`).Scan(&note, &hits); err != nil {
		t.Fatalf("insert with defaults failed: %v", err)
	}
	if !note.Valid || note.String != "hello" {
		t.Errorf("note default = %+v, want 'hello'", note)
	}
	if !hits.Valid || hits.Int64 != 42 {
		t.Errorf("hits default = %+v, want 42", hits)
	}

	table, err := conn.IntrospectTable(ctx, "kb_ddl_probe")
	if err != nil {
		t.Fatalf("IntrospectTable failed: %v", err)
	}
	if len(table.Columns) != 3 {
		t.Errorf("IntrospectTable columns = %d, want 3", len(table.Columns))
	}
	for _, col := range table.Columns {
		if col.Name == "id" && !col.IsPrimaryKey {
			t.Errorf("column id should be primary key")
		}
		if col.Name == "note" && col.Nullable != true {
			t.Errorf("column note should be nullable")
		}
	}
}

// TestKingbaseCRUDPipeline exercises the full faucet build+execute chain
// (BuildInsert/BuildUpdate/BuildCount/BuildDelete) through gokb.
func TestKingbaseCRUDPipeline(t *testing.T) {
	conn := connectForIntegration(t)
	ctx := context.Background()

	if _, err := conn.DB().ExecContext(ctx, `DROP TABLE IF EXISTS kb_crud_probe`); err != nil {
		t.Fatalf("setup drop: %v", err)
	}
	if _, err := conn.DB().ExecContext(ctx,
		`CREATE TABLE kb_crud_probe (id serial PRIMARY KEY, name text NOT NULL, age int)`); err != nil {
		t.Fatalf("setup create: %v", err)
	}
	t.Cleanup(func() { conn.DB().ExecContext(ctx, `DROP TABLE kb_crud_probe`) })

	// INSERT (multi-row, RETURNING *) via BuildInsert.
	insSQL, insArgs, err := conn.BuildInsert(ctx, connector.InsertRequest{
		Table: "kb_crud_probe",
		Records: []map[string]interface{}{
			{"name": "alice", "age": 30},
			{"name": "bob", "age": 25},
		},
	})
	if err != nil {
		t.Fatalf("BuildInsert: %v", err)
	}
	rows, err := conn.DB().QueryxContext(ctx, insSQL, insArgs...)
	if err != nil {
		t.Fatalf("exec BuildInsert (%s): %v", insSQL, err)
	}
	inserted := 0
	for rows.Next() {
		inserted++
	}
	rows.Close()
	if inserted != 2 {
		t.Fatalf("BuildInsert returned %d rows, want 2", inserted)
	}

	// UPDATE via filter. API contract: SET values take $1..$N, so the
	// filter's placeholders must continue numbering from $N+1.
	updSQL, updArgs, err := conn.BuildUpdate(ctx, connector.UpdateRequest{
		Table:      "kb_crud_probe",
		Filter:     "name = $2",
		FilterArgs: []interface{}{"alice"},
		Record:     map[string]interface{}{"age": 31},
	})
	if err != nil {
		t.Fatalf("BuildUpdate: %v", err)
	}
	res, err := conn.DB().ExecContext(ctx, updSQL, updArgs...)
	if err != nil {
		t.Fatalf("exec BuildUpdate (%s): %v", updSQL, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Errorf("update affected %d rows, want 1", n)
	}

	// COUNT with parameterized filter.
	cntSQL, cntArgs, err := conn.BuildCount(ctx, connector.CountRequest{
		Table:      "kb_crud_probe",
		Filter:     "age >= $1",
		FilterArgs: []interface{}{25},
	})
	if err != nil {
		t.Fatalf("BuildCount: %v", err)
	}
	var count int
	if err := conn.DB().QueryRowxContext(ctx, cntSQL, cntArgs...).Scan(&count); err != nil {
		t.Fatalf("exec BuildCount (%s): %v", cntSQL, err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	// DELETE via filter.
	delSQL, delArgs, err := conn.BuildDelete(ctx, connector.DeleteRequest{
		Table:      "kb_crud_probe",
		Filter:     "name = $1",
		FilterArgs: []interface{}{"bob"},
	})
	if err != nil {
		t.Fatalf("BuildDelete: %v", err)
	}
	res, err = conn.DB().ExecContext(ctx, delSQL, delArgs...)
	if err != nil {
		t.Fatalf("exec BuildDelete (%s): %v", delSQL, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Errorf("delete affected %d rows, want 1", n)
	}
}

// TestKingbaseUpsertAndTransaction covers ON CONFLICT upsert and explicit
// transaction commit/rollback semantics through gokb.
func TestKingbaseUpsertAndTransaction(t *testing.T) {
	conn := connectForIntegration(t)
	ctx := context.Background()

	if _, err := conn.DB().ExecContext(ctx, `DROP TABLE IF EXISTS kb_tx_probe`); err != nil {
		t.Fatalf("setup drop: %v", err)
	}
	if _, err := conn.DB().ExecContext(ctx,
		`CREATE TABLE kb_tx_probe (id int PRIMARY KEY, val text)`); err != nil {
		t.Fatalf("setup create: %v", err)
	}
	t.Cleanup(func() { conn.DB().ExecContext(ctx, `DROP TABLE kb_tx_probe`) })

	// Upsert: insert then conflict-update.
	if _, err := conn.DB().ExecContext(ctx,
		`INSERT INTO kb_tx_probe (id, val) VALUES ($1, $2)`, 1, "first"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := conn.DB().ExecContext(ctx,
		`INSERT INTO kb_tx_probe (id, val) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET val = EXCLUDED.val`,
		1, "updated"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var val string
	if err := conn.DB().QueryRowxContext(ctx, `SELECT val FROM kb_tx_probe WHERE id = 1`).Scan(&val); err != nil {
		t.Fatalf("select: %v", err)
	}
	if val != "updated" {
		t.Errorf("val after upsert = %q, want 'updated'", val)
	}

	// Rollback: changes inside an aborted transaction must not persist.
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kb_tx_probe SET val = $1 WHERE id = $2`, "rolled-back", 1); err != nil {
		t.Fatalf("tx update: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := conn.DB().QueryRowxContext(ctx, `SELECT val FROM kb_tx_probe WHERE id = 1`).Scan(&val); err != nil {
		t.Fatalf("select after rollback: %v", err)
	}
	if val != "updated" {
		t.Errorf("val after rollback = %q, want 'updated' (rollback leaked)", val)
	}

	// Commit: changes inside a committed transaction must persist.
	tx, err = conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx (commit): %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kb_tx_probe SET val = $1 WHERE id = $2`, "committed", 1); err != nil {
		t.Fatalf("tx update (commit): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := conn.DB().QueryRowxContext(ctx, `SELECT val FROM kb_tx_probe WHERE id = 1`).Scan(&val); err != nil {
		t.Fatalf("select after commit: %v", err)
	}
	if val != "committed" {
		t.Errorf("val after commit = %q, want 'committed'", val)
	}
}

// TestKingbaseWideTypes verifies gokb round-trips the common rich type set:
// numeric, boolean, timestamptz, jsonb, bytea and a text array.
func TestKingbaseWideTypes(t *testing.T) {
	conn := connectForIntegration(t)
	ctx := context.Background()

	if _, err := conn.DB().ExecContext(ctx, `DROP TABLE IF EXISTS kb_types_probe`); err != nil {
		t.Fatalf("setup drop: %v", err)
	}
	if _, err := conn.DB().ExecContext(ctx, `
		CREATE TABLE kb_types_probe (
			id serial PRIMARY KEY,
			price numeric(10,2),
			active boolean,
			created_at timestamptz,
			meta jsonb,
			blob bytea,
			tags text[]
		)`); err != nil {
		t.Fatalf("setup create: %v", err)
	}
	t.Cleanup(func() { conn.DB().ExecContext(ctx, `DROP TABLE kb_types_probe`) })

	ts := time.Date(2026, 9, 27, 12, 30, 0, 0, time.UTC)
	// gokb (lib/pq lineage) does not accept native Go slices as array
	// parameters the way pgx does — arrays must be passed as PG array
	// literals. This is a documented difference of the gokb path.
	tagsLiteral := `{"alpha","beta"}`
	if _, err := conn.DB().ExecContext(ctx, `
		INSERT INTO kb_types_probe (price, active, created_at, meta, blob, tags)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6)`,
		"19.95", true, ts, `{"plan":"pro","seats":5}`, []byte{0xDE, 0xAD, 0xBE, 0xEF}, tagsLiteral); err != nil {
		t.Fatalf("insert wide types: %v", err)
	}

	var (
		price     string
		active    bool
		createdAt time.Time
		meta      []byte
		blob      []byte
		tags      []byte
	)
	if err := conn.DB().QueryRowxContext(ctx,
		`SELECT price, active, created_at, meta, blob, tags FROM kb_types_probe WHERE id = 1`).
		Scan(&price, &active, &createdAt, &meta, &blob, &tags); err != nil {
		t.Fatalf("select wide types: %v", err)
	}

	if price != "19.95" {
		t.Errorf("numeric = %q, want '19.95'", price)
	}
	if !active {
		t.Errorf("boolean = false, want true")
	}
	if got := createdAt.UTC(); !got.Equal(ts) {
		t.Errorf("timestamptz = %v, want %v", got, ts)
	}
	if len(meta) == 0 || !strings.Contains(string(meta), `"plan"`) {
		t.Errorf("jsonb = %q, want object containing \"plan\"", meta)
	}
	if len(blob) != 4 || blob[0] != 0xDE || blob[3] != 0xEF {
		t.Errorf("bytea = %v, want [DE AD BE EF]", blob)
	}
	if len(tags) == 0 || !strings.Contains(string(tags), "alpha") {
		t.Errorf("text[] = %q, want array containing 'alpha'", tags)
	}
}

// TestKingbaseStoredProcedures verifies the stored-procedure introspection
// path works against KingbaseES in PG mode (pg_proc compatibility).
func TestKingbaseStoredProcedures(t *testing.T) {
	conn := connectForIntegration(t)
	ctx := context.Background()

	if _, err := conn.DB().ExecContext(ctx, `DROP FUNCTION IF EXISTS kb_probe_add(int, int)`); err != nil {
		t.Fatalf("setup drop function: %v", err)
	}
	if _, err := conn.DB().ExecContext(ctx,
		`CREATE FUNCTION kb_probe_add(a int, b int) RETURNS int AS 'SELECT a + b' LANGUAGE SQL`); err != nil {
		t.Fatalf("setup create function: %v", err)
	}
	t.Cleanup(func() { conn.DB().ExecContext(ctx, `DROP FUNCTION IF EXISTS kb_probe_add(int, int)`) })

	procs, err := conn.GetStoredProcedures(ctx)
	if err != nil {
		t.Fatalf("GetStoredProcedures failed: %v", err)
	}
	found := false
	for _, p := range procs {
		if p.Name == "kb_probe_add" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("kb_probe_add not found among %d stored procedures", len(procs))
	}
}
