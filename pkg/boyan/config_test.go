package boyan_test

import boyan "github.com/baldaworks/boyan/pkg/boyan"

import "testing"

func TestDefaultConfigValidate(t *testing.T) {
	requireNoError(t, boyan.DefaultConfig().Validate(), "DefaultConfig().Validate() error = %v")
}

func TestValidateRejectsNonLoopbackListener(t *testing.T) {
	config := boyan.DefaultConfig()

	config.HTTP.Listen = "0.0.0.0:8686"
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want loopback validation failure")
	}
}
