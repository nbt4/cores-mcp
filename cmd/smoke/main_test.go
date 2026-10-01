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
		"warehouse.packages.archive", "warehouse.packages.restore", "warehouse.devices.create", "warehouse.devices.update", "warehouse.devices.archive", "warehouse.devices.restore", "warehouse.devices.revert_update",
		"rental.jobs.assign_device",
		"rental.jobs.update",
		"rental.requirements.update",
		"procurement.orders.create",
		"warehouse.movements.create",
		"warehouse.devices.update_status",
		"warehouse.manufacturers.update",
		"warehouse.brands.update",
		"warehouse.categories.delete",
		"warehouse.subcategories.delete",
		"warehouse.third_categories.delete",
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

func TestReadSmokeArgumentsMatchMasterAndAuditSchemas(t *testing.T) {
	for _, test := range []struct{ name, required string }{
		{"cores.master_data.resolve", "entity"},
		{"warehouse.audit.history", "product_id"},
		{"warehouse.devices.audit_history", "device_id"},
		{"warehouse.packages.audit_history", "package_id"},
		{"procurement.audit.history", "id"},
	} {
		arguments := smokeArguments(test.name)
		if arguments[test.required] == nil {
			t.Fatalf("%s missing %s: %#v", test.name, test.required, arguments)
		}
		if test.name != "cores.master_data.resolve" && arguments["query"] != nil {
			t.Fatalf("audit smoke contains unsupported query: %#v", arguments)
		}
	}
}
