// Package canonicalsqltest supplies isolated real SQL connections for canonical
// storage tests. Public owner factories live in its storefixture subpackage.
package canonicalsqltest

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

func configuredPostgres() (string, error) {
	required := false

	if value := os.Getenv("BOYAN_REQUIRE_POSTGRES"); value != "" {
		var err error

		required, err = strconv.ParseBool(value)
		if err != nil {
			return "", errors.New("BOYAN_REQUIRE_POSTGRES must be a boolean")
		}
	}

	dsn := strings.TrimSpace(os.Getenv("BOYAN_TEST_POSTGRES_DSN"))
	if required && dsn == "" {
		return "", errors.New("BOYAN_REQUIRE_POSTGRES requires BOYAN_TEST_POSTGRES_DSN")
	}

	return dsn, nil
}
