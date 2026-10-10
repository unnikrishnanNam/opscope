package snapshot

import (
	"bytes"
	"os"
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// The manifests grant only what Opscope reads. A kind added to the snapshot
// but not to the ClusterRole would be "forbidden" in every cluster that uses
// them, so this checks the two agree.
func TestClusterRolesCoverSnapshot(t *testing.T) {
	granted := map[string]bool{}
	for _, file := range []string{"opscope.yaml", "secrets-access.yaml"} {
		data, err := os.ReadFile("../../../deploy/kubernetes/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range bytes.Split(data, []byte("\n---\n")) {
			var role rbacv1.ClusterRole
			if err := yaml.Unmarshal(doc, &role); err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			if role.Kind != "ClusterRole" {
				continue
			}
			for _, rule := range role.Rules {
				if slices.Contains(rule.Verbs, "list") {
					for _, resource := range rule.Resources {
						granted[resource] = true
					}
				}
			}
		}
	}

	for _, resource := range Resources() {
		if !granted[resource] {
			t.Errorf("the snapshot reads %s, but no ClusterRole in deploy/kubernetes/ lets Opscope list it", resource)
		}
	}
}
