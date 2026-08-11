package standaloneruntime

import (
	"github.com/baldaworks/boyan/internal/forgetops"
	memoryops "github.com/baldaworks/boyan/internal/memoryops"
	memoryread "github.com/baldaworks/boyan/internal/memoryread"
)

func buildStandaloneReadServices(storage *standaloneStorage) (*memoryread.RecallService, *memoryread.TraceService, *memoryops.ForgetService, error) {
	recall, err := memoryread.NewRecallService(storage.reader, storage.index)
	if err != nil {
		return nil, nil, nil, err
	}
	trace, err := memoryread.NewTraceService(storage.reader)
	if err != nil {
		return nil, nil, nil, err
	}
	forgetOperations, err := forgetops.NewCanonicalForgetAdapter(storage.store)
	if err != nil {
		return nil, nil, nil, err
	}
	forget, err := memoryops.NewForgetServiceWithProjection(forgetOperations, storage.projection)
	if err != nil {
		return nil, nil, nil, err
	}
	return recall, trace, forget, nil
}
