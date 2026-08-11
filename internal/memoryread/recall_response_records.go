package memoryread

import (
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *RecallService) responseFromRecords(request memory.RecallRequest, records []memory.RecallRecord, currentSeq uint64, now time.Time) (memory.RecallResponse, error) {
	results := make([]memory.RecallReference, 0, request.Limit)
	seen := make(map[string]struct{}, len(records))
	tokens := strings.Fields(strings.ToLower(request.Query))
	for _, record := range records {
		if _, exists := seen[record.RevisionID]; exists {
			continue
		}
		if err := record.Validate(request, now); err != nil {
			if skipStaleRecallError(err) {
				continue
			}
			return memory.RecallResponse{}, err
		}
		matched := 0
		lowerText := strings.ToLower(record.Text)
		for _, token := range tokens {
			if strings.Contains(lowerText, token) {
				matched++
			}
		}
		if matched == 0 {
			continue
		}
		score := float64(matched) / float64(len(tokens))
		results = append(results, recallReference(record, memory.RankRecall(score, record.CreatedAt, now)))
		seen[record.RevisionID] = struct{}{}
		if len(results) == request.Limit {
			break
		}
	}
	memory.SortRecallReferences(results)
	return recallResponse(request.Scope, currentSeq, results), nil
}
