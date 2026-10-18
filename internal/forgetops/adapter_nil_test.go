package forgetops_test

import (
	"github.com/baldaworks/boyan/internal/forgetops"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
	"testing"
)

func TestForgetAdapterRejectsTypedNilStore(t *testing.T) {
	for _, store := range []forgetops.CanonicalForgetStore{nil, (*sqlite.Store)(nil), (*postgres.Store)(nil)} {
		if adapter, err := forgetops.NewCanonicalForgetAdapter(store); err == nil || adapter != nil {
			t.Fatal("nil canonical authority accepted")
		}
	}
}
