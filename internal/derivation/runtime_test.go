package derivation

import (
	"context"
	"testing"

	portableapp "github.com/baldaworks/boyan/internal/apps/boyan"
	adkagent "google.golang.org/adk/v2/agent"
	adksession "google.golang.org/adk/v2/session"
)

type presetInvoker struct{}

func (*presetInvoker) Invoke(context.Context, portableapp.StructuredInvocation) ([]byte, error) {
	return nil, nil
}

func (*presetInvoker) Close(context.Context) error { return nil }

func TestNewRuntimeComposesThroughPortableAppRuntime(t *testing.T) {
	t.Parallel()

	runtime, err := newRuntimeWithFactory(
		testConfig(t),
		portableapp.RuntimeConfig{},
		&fakeFactory{},
		func(adkagent.Agent, adksession.Service, string) (eventRunner, error) {
			return fakeRunner{}, nil
		},
	)
	if err != nil {
		t.Fatalf("newRuntimeWithFactory() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Runtime.Start() error = %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Runtime.Close() error = %v", err)
	}
}

func TestNewRuntimeRejectsPrewiredDerivationOwners(t *testing.T) {
	t.Parallel()

	_, err := newRuntimeWithFactory(
		testConfig(t),
		portableapp.RuntimeConfig{StructuredInvoker: &presetInvoker{}},
		&fakeFactory{},
		func(adkagent.Agent, adksession.Service, string) (eventRunner, error) {
			return fakeRunner{}, nil
		},
	)
	if err == nil {
		t.Fatal("newRuntimeWithFactory() error = nil, want ownership guard")
	}
}
