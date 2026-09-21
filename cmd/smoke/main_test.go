package main

import "testing"

func TestRequirementSmokeArgumentsExerciseManufacturerLookupWithoutMutation(t *testing.T) {
	for _, name := range []string{"rental.requirements.prepare_create", "rental.requirements.create"} {
		arguments := smokeArguments(name)
		if arguments["job_id"] != 1160 || arguments["product_id"] != 4 || arguments["quantity"] != 1 {
			t.Fatalf("%s arguments = %#v", name, arguments)
		}
		if confirmed, ok := arguments["confirm_creation"]; ok && confirmed == true {
			t.Fatalf("%s smoke test must not confirm a mutation", name)
		}
	}
}

func TestWriteSmokeArgumentsNeverConfirmMutation(t *testing.T) {
	for _, name := range []string{
		"procurement.products.create",
		"rental.jobs.create",
		"planner.tasks.create",
		"warehouse.tasks.create",
		"rental.jobs.assign_device",
		"rental.jobs.update",
		"rental.requirements.update",
		"procurement.orders.create",
		"warehouse.movements.create",
		"warehouse.devices.update_status",
	} {
		arguments := smokeArguments(name)
		if name == "rental.requirements.update" && len(arguments) != 0 {
			t.Fatalf("%s arguments = %#v, want empty safe draft", name, arguments)
		}
		for key, value := range arguments {
			if value == true {
				t.Fatalf("%s sets %s=true", name, key)
			}
		}
	}
}
