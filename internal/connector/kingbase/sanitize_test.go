package kingbase

import (
	"testing"

	"github.com/faucetdb/faucet/internal/connector"
)

// TestSanitizeDSNKingbase verifies URL-style kingbase DSNs get their password
// percent-encoded (the shared URL sanitizer covers the kingbase driver).
// Lives in this package because the connector package's TestMain gates its
// whole test binary behind FAUCET_INTEGRATION, which would skip these
// assertions in CI.
func TestSanitizeDSNKingbase(t *testing.T) {
	in := "kingbase://user:p#ss@host:54321/db"
	got := connector.SanitizeDSN("kingbase", in)
	want := "kingbase://user:p%23ss@host:54321/db"
	if got != want {
		t.Errorf("SanitizeDSN(kingbase, %q) = %q, want %q", in, got, want)
	}
}

// TestSanitizeDSNKingbaseNonURL verifies lib/pq-style keyword DSNs pass
// through unchanged.
func TestSanitizeDSNKingbaseNonURL(t *testing.T) {
	in := "user=test password=test host=localhost port=54321 dbname=test"
	got := connector.SanitizeDSN("kingbase", in)
	if got != in {
		t.Errorf("SanitizeDSN(kingbase, non-URL) = %q, want unchanged %q", got, in)
	}
}
