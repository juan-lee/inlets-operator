package provision

import "testing"

func TestParseID(t *testing.T) {
	id := "/subscriptions/fea5a321-93e4-4a5b-9f44-8aefa414257d/resourceGroups/inletsgroup/providers/Microsoft.ContainerInstance/containerGroups/inlets"
	subID, rg, name := parseContainerGroupID(id)
	if subID != "fea5a321-93e4-4a5b-9f44-8aefa414257d" {
		t.Errorf("Incorrect subscription ID %s", subID)
	}
	if rg != "inletsgroup" {
		t.Errorf("Incorrect resource group name %s", rg)
	}
	if name != "inlets" {
		t.Errorf("Incorrect container instance name %s", name)
	}
}
