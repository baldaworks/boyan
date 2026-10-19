package standaloneruntime

import (
	"context"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type readinessChecker struct {
	reporter readinessErrorReporter
}

func (c readinessChecker) CheckReadiness(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("readiness context is required")
	}

	if c.reporter != nil {
		if err := c.reporter.LastError(); err != nil {
			return err
		}
	}

	return nil
}

type standaloneReadinessReporter struct {
	service *standaloneService
}

func (reporter *standaloneReadinessReporter) LastError() error {
	if reporter == nil || reporter.service == nil || !reporter.service.ready() {
		return memory.PermanentError(memory.CodeUnavailable, "Boyan service is not ready", nil)
	}

	return nil
}

func (reporter *standaloneReadinessReporter) attach(service *standaloneService) {
	if reporter == nil {
		return
	}

	reporter.service = service
}
