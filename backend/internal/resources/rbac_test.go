package resources

import (
	"os"
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// The manifests grant get, list and watch on every kind a collector keeps,
// so adding a kind without its permission fails here rather than as a
// "forbidden" on someone's cluster. Secrets come from secrets-access.yaml.
func TestClusterRolesCoverEveryKind(t *testing.T) {
	type grant struct{ group, resource string }
	verbs := map[grant][]string{}
	for _, file := range []string{"opscope.yaml", "secrets-access.yaml"} {
		raw, err := os.ReadFile("../../../deploy/kubernetes/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range strings.Split(string(raw), "\n---") {
			var role rbacv1.ClusterRole
			if err := yaml.Unmarshal([]byte(doc), &role); err != nil || role.Kind != "ClusterRole" {
				continue
			}
			for _, rule := range role.Rules {
				for _, g := range rule.APIGroups {
					for _, r := range rule.Resources {
						key := grant{g, r}
						verbs[key] = append(verbs[key], rule.Verbs...)
					}
				}
			}
		}
	}
	for _, k := range Kinds() {
		got := verbs[grant{k.GVR.Group, k.Resource}]
		for _, verb := range []string{"get", "list", "watch"} {
			if !slices.Contains(got, verb) {
				t.Errorf("%s (%q): no %q in the manifests' ClusterRoles", k.Resource, k.GVR.Group, verb)
			}
		}
	}
}
