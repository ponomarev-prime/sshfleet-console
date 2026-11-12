package containers

import (
	"encoding/json"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
)

func TestInspectionHealthDistinguishesAbsentUnknownAndMeasured(t *testing.T) {
	for _, tc := range []struct{ name, data, want string }{
		{"docker absent", `{"Config":{},"State":{}}`, "not-configured"},
		{"podman empty health", `{"Config":{},"State":{"Health":{"Status":""}}}`, "not-configured"},
		{"podman healthcheck name", `{"Config":{},"State":{"Healthcheck":{"Status":"healthy"}}}`, "healthy"},
		{"configured unknown", `{"Config":{"Healthcheck":{"Test":["CMD","true"]}},"State":{"Health":{}}}`, "unknown"},
		{"disabled check", `{"Config":{"Healthcheck":{"Test":["NONE"]}},"State":{"Health":{}}}`, "not-configured"},
		{"unhealthy", `{"Config":{},"State":{"Health":{"Status":"unhealthy"}}}`, "unhealthy"},
		{"starting", `{"Config":{},"State":{"Health":{"Status":"starting"}}}`, "starting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var record inspectRecord
			if err := json.Unmarshal([]byte(tc.data), &record); err != nil {
				t.Fatal(err)
			}
			var host inventory.Host
			applyInspection(&host, record)
			if host.ContainerHealth != tc.want {
				t.Fatalf("health=%q, want %q", host.ContainerHealth, tc.want)
			}
		})
	}
}
