package derivation //nolint:testpackage // Exercises eventRunner within the package.

import (
	"context"
	"testing"

	portableapp "github.com/baldaworks/boyan/internal/portableapp"
	"github.com/baldaworks/boyan/internal/structuredderive"
	adkagent "google.golang.org/adk/v2/agent"
)

type presetInvoker struct{}

func (*presetInvoker) Invoke(context.Context, structuredderive.StructuredInvocation) ([]byte, error) {
	return nil, nil
}

func (*presetInvoker) Close(context.Context) error { return nil }

func TestNewRuntimeComposesThroughPortableAppRuntime(t *testing.T) {
	t.Parallel()

	runtime, err := newRuntimeWithFactory(
		testConfig(t),
		portableapp.RuntimeConfig{},
		&fakeFactory{},
		func(adkagent.Agent, runStateStore, string) (eventRunner, error) {
			return fakeRunner{}, nil
		},
	)
	requireNoError(t, err, "newRuntimeWithFactory() error = %v")

	requireNoError(t, runtime.Start(context.Background()), "Runtime.Start() error = %v")

	requireNoError(t, runtime.Close(context.Background()), "Runtime.Close() error = %v")
}

func TestNewRuntimeRejectsPrewiredDerivationOwners(t *testing.T) {
	t.Parallel()

	_, err := newRuntimeWithFactory(
		testConfig(t),
		portableapp.RuntimeConfig{StructuredInvoker: &presetInvoker{}},
		&fakeFactory{},
		func(adkagent.Agent, runStateStore, string) (eventRunner, error) {
			return fakeRunner{}, nil
		},
	)
	if err == nil {
		t.Fatal("newRuntimeWithFactory() error = nil, want ownership guard")
	}
}
