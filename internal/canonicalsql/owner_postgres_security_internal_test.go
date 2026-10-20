package canonicalsql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPostgresOwnerTLSPolicy(t *testing.T) {
	cases := []struct {
		name  string
		dsn   string
		local bool
		valid bool
	}{
		{"verified_remote", "host=database.example sslmode=verify-full", false, true},
		{"remote_disable", "host=database.example sslmode=disable", true, false},
		{"local_requires_opt_in", "host=127.0.0.1 sslmode=disable", false, false},
		{"loopback_disable", "host=127.0.0.1 sslmode=disable", true, true},
		{"ipv6_loopback", "host=::1 sslmode=disable", true, true},
		{"unix_disable", "host=/tmp sslmode=disable", true, true},
		{"localhost_disable", "host=localhost sslmode=disable", true, true},
		{"prefer_downgrade", "host=localhost sslmode=prefer", true, false},
		{"allow_downgrade", "host=localhost sslmode=allow", true, false},
		{"require_without_identity", "host=database.example sslmode=require", false, false},
		{"verify_ca_without_identity", "host=database.example sslmode=verify-ca", false, false},
		{"second_host", "host=127.0.0.1,database.example sslmode=disable", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := PostgresConfig{DSN: tc.dsn, AllowLocalInsecure: tc.local}

			native, err := config.parse()
			if (err == nil) != tc.valid {
				t.Fatalf("TLS policy accepted=%v, want %v", err == nil, tc.valid)
			}

			if err == nil && native.TLSConfig != nil &&
				(native.TLSConfig.ServerName != "database.example" || native.TLSConfig.InsecureSkipVerify) {
				t.Fatal("verified identity was lost")
			}
		})
	}
}

func TestPostgresOwnerConfigAndErrorsArePrivate(t *testing.T) {
	secret := "private-test-credential"
	for _, config := range []PostgresConfig{
		{}, {DSN: "host='" + secret},
		{DSN: "host=127.0.0.1 password=" + secret + " sslmode=disable"},
		{DSN: "host=127.0.0.1 sslmode=verify-full sslrootcert=/missing/" + secret},
		{DSN: "host=127.0.0.1 sslmode=disable", AllowLocalInsecure: true, ConnectTimeout: -time.Second},
		{DSN: "host=127.0.0.1 sslmode=disable", AllowLocalInsecure: true, ConnectTimeout: 31 * time.Second},
	} {
		owner, err := OpenPostgresOwner(t.Context(), config)
		if owner != nil || err == nil {
			t.Fatal("invalid config admitted")
		}

		for _, output := range []string{err.Error(), fmt.Sprintf("%v %+v %#v", config, config, config)} {
			if strings.Contains(output, secret) {
				t.Fatal("private configuration leaked")
			}
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := OpenPostgresOwner(ctx, PostgresConfig{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open lost identity: %v", err)
	}
}
