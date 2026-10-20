package sqlite

import (
	"math"
	"testing"
)

func TestCanonicalAccountingIncludesNormalizedDuplicatedTokensAndOverflow(t *testing.T) {
	r := applyRecord("r")

	input, err := canonicalLexicalInput(r)
	if err != nil {
		t.Fatal(err)
	}

	n, err := input.encodedBytes("b")
	// 35 identity/rowid binds + 105 scalar bytes + 32 repeated FTS token
	// bytes + 14 source membership bytes + 20 conversation membership bytes.
	if err != nil || n != 206 {
		t.Fatalf("ordinary encoded inputs: %d %v", n, err)
	}

	r.Text = "cafe\u0301 кошки running"

	input, err = canonicalLexicalInput(r)
	if err != nil {
		t.Fatal(err)
	}

	n, err = input.encodedBytes("b")
	// NFC raw=24 bytes, RU=8, EN=9, each sent twice; +142 other bytes.
	if err != nil || n != 224 {
		t.Fatalf("normalized encoded inputs: %d %v", n, err)
	}

	if _, err = checkedInputBytes(math.MaxUint64, 1); err == nil {
		t.Fatal("accounting wrapped")
	}

	if n, err = checkedInputBytes(math.MaxUint64-1, 1); err != nil || n != math.MaxUint64 {
		t.Fatal("at-limit accounting failed", n, err)
	}
}
