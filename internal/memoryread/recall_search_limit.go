package memoryread

import memory "github.com/baldaworks/boyan/pkg/memory"

const recallCandidateMultiplier = 4

func recallTailLimit(limit int) uint32 {
	if limit < 1 {
		return 1
	}

	want := limit * recallCandidateMultiplier
	if want < limit {
		want = limit
	}

	if want > memory.MaxRecallCandidates {
		want = memory.MaxRecallCandidates
	}

	return uint32(want)
}
