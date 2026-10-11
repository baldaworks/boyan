package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"bytes"
	"testing"
)

func TestTupleKeyUsesUnambiguousLengthPrefixes(t *testing.T) {
	t.Parallel()

	left, err := memory.TupleKey(1, 1, "ab", "c")
	if err != nil {
		t.Fatalf("TupleKey() left error = %v", err)
	}

	right, err := memory.TupleKey(1, 1, "a", "bc")
	if err != nil {
		t.Fatalf("TupleKey() right error = %v", err)
	}

	if bytes.Equal(left, right) {
		t.Fatalf("ambiguous tuple keys: %x", left)
	}

	if got, want := left, []byte{1, 1, 0, 2, 'a', 'b', 0, 1, 'c'}; !bytes.Equal(got, want) {
		t.Fatalf("tuple key = %x, want %x", got, want)
	}
}

func FuzzTupleKey(f *testing.F) {
	f.Add("scope-1", "item-1")
	f.Add("", "item-1")
	f.Fuzz(func(t *testing.T, first, second string) {
		key, err := memory.TupleKey(1, 1, first, second)
		if err != nil {
			return
		}

		if len(key) < 6 || key[0] != 1 || key[1] != 1 {
			t.Fatalf("tuple key = %x", key)
		}
	})
}
