package boyan

import "testing"

func TestDefaultConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() error = %v", err)
	}
}

func TestValidateRejectsNonLoopbackListener(t *testing.T) {
	config := DefaultConfig()
	config.HTTP.Listen = "0.0.0.0:8686"
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want loopback validation failure")
	}
}
