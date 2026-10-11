package derivation

import (
	"context"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	adkrunstate "google.golang.org/adk/v2/session"
)

type runEvent = adkrunstate.Event

type runStateStore struct {
	service adkrunstate.Service
}

func newRunStateStore() runStateStore {
	return runStateStore{service: adkrunstate.InMemoryService()}
}

func createRunState(ctx context.Context, store runStateStore, appName, stateID string) error {
	_, err := store.service.Create(ctx, &adkrunstate.CreateRequest{
		AppName:   appName,
		UserID:    defaultDerivationUser,
		SessionID: stateID,
	})

	return err
}

func deleteRunState(ctx context.Context, store runStateStore, appName, stateID string) {
	_ = store.service.Delete(ctx, &adkrunstate.DeleteRequest{
		AppName:   appName,
		UserID:    defaultDerivationUser,
		SessionID: stateID,
	})
}

func getRunState(ctx context.Context, store runStateStore, appName, stateID string) error {
	_, err := store.service.Get(ctx, &adkrunstate.GetRequest{
		AppName:   appName,
		UserID:    defaultDerivationUser,
		SessionID: stateID,
	})

	return err
}

func newRunEventRunner(agent adkagent.Agent, stateStore runStateStore, appName string) (eventRunner, error) {
	return runner.New(runner.Config{
		AppName:        appName,
		Agent:          agent,
		SessionService: stateStore.service,
	})
}
