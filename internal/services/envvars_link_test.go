package services

import (
	"encoding/json"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestLinkEnvVars(t *testing.T) {
	inst := &models.ServiceInstance{
		Name:           "cache",
		TemplateName:   "redis",
		ConnectionInfo: json.RawMessage(`{"url":"redis://cache:6379","host":"cache"}`),
	}
	got, err := LinkEnvVars(inst)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[ServiceEnvVarName("cache", "redis", "url")] != "redis://cache:6379" {
		t.Errorf("url var missing: %v", got)
	}

	if _, badErr := LinkEnvVars(&models.ServiceInstance{ConnectionInfo: json.RawMessage(`not json`)}); badErr == nil {
		t.Error("bad connection info accepted")
	}
	empty, err := LinkEnvVars(&models.ServiceInstance{})
	if err != nil || len(empty) != 0 {
		t.Errorf("empty: %v %v", empty, err)
	}
}
