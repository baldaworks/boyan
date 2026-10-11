package portableapp_test

import portableapp "github.com/baldaworks/boyan/internal/portableapp"

import (
	"context"
	"reflect"
	"testing"
)

type lifecycleSpy struct {
	name  string
	log   *[]string
	start bool
}

func (s *lifecycleSpy) Start(context.Context) error {
	*s.log = append(*s.log, "start:"+s.name)
	s.start = true

	return nil
}

func (s *lifecycleSpy) Close(context.Context) error {
	*s.log = append(*s.log, "close:"+s.name)

	return nil
}

func TestRuntimeStartsAndClosesLifecycleInOrder(t *testing.T) {
	t.Parallel()

	var log []string

	first := &lifecycleSpy{name: "first", log: &log}
	second := &lifecycleSpy{name: "second", log: &log}

	runtime, err := portableapp.NewRuntime(portableapp.RuntimeConfig{Lifecycle: []portableapp.Lifecycle{first, second}})
	requireNoError(t, err, "NewRuntime() error = %v")

	requireNoError(t, runtime.Start(context.Background()), "Start() error = %v")

	requireNoError(t, runtime.Start(context.Background()), "idempotent Start() error = %v")

	requireNoError(t, runtime.Close(context.Background()), "Close() error = %v")

	requireNoError(t, runtime.Close(context.Background()), "idempotent Close() error = %v")

	want := []string{"start:first", "start:second", "close:second", "close:first"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("lifecycle log = %v, want %v", log, want)
	}
}
