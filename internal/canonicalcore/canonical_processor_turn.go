package canonicalcore

import . "github.com/baldaworks/boyan/pkg/memory"

func prepareCanonicalTurn(turn Turn) (Turn, error) {
	return NormalizeCanonicalTurn(turn)
}
