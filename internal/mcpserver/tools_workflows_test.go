package mcpserver

import (
	"context"
	"testing"
)

func TestWorkflowPreparationsRejectMissingInputsWithoutWrites(t *testing.T) {
	tests := []struct {
		name    string
		prepare func() (preparedMutation, error)
		missing []string
	}{
		{
			name: "requirement update",
			prepare: func() (preparedMutation, error) {
				return prepareRequirementUpdate(context.Background(), nil, RequirementUpdateInput{})
			},
			missing: []string{"requirement_id", "quantity"},
		},
		{
			name: "warehouse movement",
			prepare: func() (preparedMutation, error) {
				return prepareWarehouseMovementCreate(context.Background(), nil, WarehouseMovementCreateInput{})
			},
			missing: []string{"action", "scan_code"},
		},
		{
			name: "device status",
			prepare: func() (preparedMutation, error) {
				return prepareDeviceStatusUpdate(context.Background(), nil, DeviceStatusUpdateInput{})
			},
			missing: []string{"device_id"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := test.prepare()
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Ready {
				t.Fatalf("preparation unexpectedly ready: %#v", prepared)
			}
			for _, field := range test.missing {
				if !containsString(prepared.Missing, field) {
					t.Fatalf("missing fields %v do not contain %q", prepared.Missing, field)
				}
			}
		})
	}
}

func TestClosedJobStatuses(t *testing.T) {
	for _, status := range []string{"Abgeschlossen", "Storniert", "cancelled", "completed"} {
		if !isClosedStatus(status) {
			t.Fatalf("%q should be closed", status)
		}
	}
	for _, status := range []string{"Planung", "Bestätigt", "open"} {
		if isClosedStatus(status) {
			t.Fatalf("%q should remain open", status)
		}
	}
}
