package resources

// Detail sections for storage: what a claim, a volume and a StorageClass
// say about themselves, how pods and StatefulSets use storage, and the
// links between all of them.

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func claimLink(namespace, name string) *Link {
	return &Link{Resource: "persistentvolumeclaims", Namespace: namespace, Name: name}
}

func volumeLink(name string) *Link {
	return &Link{Resource: "persistentvolumes", Name: name}
}

func classLink(name string) *Link {
	return &Link{Resource: "storageclasses", Name: name}
}

func claimSections(d *Detail, obj *unstructured.Unstructured) error {
	var pvc corev1.PersistentVolumeClaim
	if err := convert(obj, &pvc); err != nil {
		return err
	}
	d.add("Status", storageStatus(pvc.ObjectMeta, string(pvc.Status.Phase)))
	d.addLink("Volume", pvc.Spec.VolumeName, volumeLink(pvc.Spec.VolumeName))
	class := claimClass(&pvc)
	d.addLink("StorageClass", class, classLink(class))
	if requested := pvc.Spec.Resources.Requests.Storage(); !requested.IsZero() {
		d.add("Requested", sizeText(requested.Value()))
	}
	if size, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
		d.add("Capacity", sizeText(size.Value()))
	}
	d.add("Access modes", accessModeNames(pvc.Spec.AccessModes))
	d.add("Volume mode", volumeMode(pvc.Spec.VolumeMode))
	return nil
}

func volumeSections(d *Detail, obj *unstructured.Unstructured) error {
	var pv corev1.PersistentVolume
	if err := convert(obj, &pv); err != nil {
		return err
	}
	d.add("Status", storageStatus(pv.ObjectMeta, string(pv.Status.Phase)))
	d.add("Reason", pv.Status.Reason)
	d.add("Message", pv.Status.Message)
	if ref := pv.Spec.ClaimRef; ref != nil {
		claim := ref.Namespace + "/" + ref.Name
		// Only a bound volume's claim still exists; a Released volume
		// remembers a claim that has been deleted, so there's nothing to open.
		if pv.Status.Phase == corev1.VolumeBound {
			d.addLink("Claim", claim, claimLink(ref.Namespace, ref.Name))
		} else {
			d.add("Claim", claim+" (no longer bound)")
		}
	}
	class := volumeClass(&pv)
	d.addLink("StorageClass", class, classLink(class))
	if size, ok := pv.Spec.Capacity[corev1.ResourceStorage]; ok {
		d.add("Capacity", sizeText(size.Value()))
	}
	d.add("Access modes", accessModeNames(pv.Spec.AccessModes))
	d.add("Reclaim policy", string(pv.Spec.PersistentVolumeReclaimPolicy))
	d.add("Volume mode", volumeMode(pv.Spec.VolumeMode))
	d.add("Source", volumeSource(pv.Spec.PersistentVolumeSource))
	d.add("Mount options", strings.Join(pv.Spec.MountOptions, ", "))
	if a := pv.Spec.NodeAffinity; a != nil {
		d.add("Node affinity", nodeSelectorText(a.Required))
	}
	return sourceTable(d, pv.Spec.PersistentVolumeSource)
}

// sourceTable lists the settings of a volume's source (a CSI driver's
// volume handle, an NFS server and path, ...). Every source type has
// different fields, so they're read generically, as the YAML would show
// them, rather than one case per type.
func sourceTable(d *Detail, src corev1.PersistentVolumeSource) error {
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&src)
	if err != nil {
		return err
	}
	settings := Table{Title: "Source settings", Columns: []string{"Setting", "Value"}}
	for _, source := range raw { // exactly one source type is set
		if fields, ok := source.(map[string]any); ok {
			flatten("", fields, &settings)
		}
	}
	sort.Slice(settings.Rows, func(i, j int) bool { return settings.Rows[i][0] < settings.Rows[j][0] })
	d.addTable(settings)
	return nil
}

// flatten adds one row per value, with nested keys joined by dots
// ("nodePublishSecretRef.name").
func flatten(prefix string, fields map[string]any, t *Table) {
	for key, value := range fields {
		switch v := value.(type) {
		case map[string]any:
			flatten(prefix+key+".", v, t)
		case []any:
			parts := []string{}
			for _, item := range v {
				parts = append(parts, fmt.Sprint(item))
			}
			t.addRow(text(prefix+key), text(strings.Join(parts, ", ")))
		case string:
			if v != "" { // an empty setting says nothing
				t.addRow(text(prefix+key), text(v))
			}
		default:
			t.addRow(text(prefix+key), text(fmt.Sprint(v)))
		}
	}
}

func storageClassSections(d *Detail, obj *unstructured.Unstructured) error {
	var sc storagev1.StorageClass
	if err := convert(obj, &sc); err != nil {
		return err
	}
	d.add("Provisioner", sc.Provisioner)
	d.add("Default", yesNo(isDefaultClass(sc.Annotations)))
	reclaim := corev1.PersistentVolumeReclaimDelete
	if sc.ReclaimPolicy != nil {
		reclaim = *sc.ReclaimPolicy
	}
	d.add("Reclaim policy", string(reclaim))
	binding := storagev1.VolumeBindingImmediate
	if sc.VolumeBindingMode != nil {
		binding = *sc.VolumeBindingMode
	}
	d.add("Binding mode", string(binding))
	d.add("Volume expansion", yesNo(sc.AllowVolumeExpansion != nil && *sc.AllowVolumeExpansion))
	d.add("Mount options", strings.Join(sc.MountOptions, ", "))

	params := Table{Title: "Parameters", Columns: []string{"Parameter", "Value"}}
	for _, key := range sortedKeys(sc.Parameters) {
		params.addRow(text(key), text(sc.Parameters[key]))
	}
	d.addTable(params)
	return nil
}

// podVolumes lists a pod's volumes: what each one is, what it points to,
// and where each container mounts it.
func podVolumes(pod *corev1.Pod) Table {
	t := Table{Title: "Volumes", Columns: []string{"Volume", "Type", "Source", "Mounted at"}}
	containers := append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...)
	for _, v := range pod.Spec.Volumes {
		mounts := []string{}
		for _, c := range containers {
			for _, m := range c.VolumeMounts {
				if m.Name != v.Name {
					continue
				}
				mount := c.Name + ": " + m.MountPath
				if m.SubPath != "" {
					mount += " (subPath " + m.SubPath + ")"
				}
				if m.ReadOnly {
					mount += " (read-only)"
				}
				mounts = append(mounts, mount)
			}
		}
		t.addRow(text(v.Name), text(volumeType(v.VolumeSource)), podVolumeSource(pod, v), text(strings.Join(mounts, "; ")))
	}
	return t
}

// volumeType names the kind of volume ("PersistentVolumeClaim",
// "ConfigMap", "EmptyDir", ...) from whichever source field is set, the
// same way volumeSource does for PVs.
func volumeType(src corev1.VolumeSource) string {
	v := reflect.ValueOf(src)
	for i := range v.NumField() {
		if f := v.Field(i); f.Kind() == reflect.Pointer && !f.IsNil() {
			return v.Type().Field(i).Name
		}
	}
	return ""
}

// podVolumeSource says what a pod volume points to, linked when Opscope
// has a page for it.
func podVolumeSource(pod *corev1.Pod, v corev1.Volume) Cell {
	ns := pod.Namespace
	switch s := v.VolumeSource; {
	case s.PersistentVolumeClaim != nil:
		name := s.PersistentVolumeClaim.ClaimName
		return Cell{Text: name, Link: claimLink(ns, name)}
	case s.Ephemeral != nil:
		// A generic ephemeral volume gets a claim named <pod>-<volume>,
		// made with the pod and deleted with it.
		name := pod.Name + "-" + v.Name
		return Cell{Text: name + " (ephemeral)", Link: claimLink(ns, name)}
	case s.ConfigMap != nil:
		return Cell{Text: s.ConfigMap.Name, Link: &Link{Resource: "configmaps", Namespace: ns, Name: s.ConfigMap.Name}}
	case s.Secret != nil:
		return Cell{Text: s.Secret.SecretName, Link: &Link{Resource: "secrets", Namespace: ns, Name: s.Secret.SecretName}}
	case s.EmptyDir != nil:
		where := "node disk"
		if s.EmptyDir.Medium == corev1.StorageMediumMemory {
			where = "memory"
		}
		if limit := s.EmptyDir.SizeLimit; limit != nil {
			where += ", up to " + limit.String()
		}
		return text(where)
	case s.HostPath != nil:
		return text(s.HostPath.Path)
	case s.Projected != nil:
		parts := []string{}
		for _, p := range s.Projected.Sources {
			switch {
			case p.ServiceAccountToken != nil:
				parts = append(parts, "service account token")
			case p.ConfigMap != nil:
				parts = append(parts, "ConfigMap "+p.ConfigMap.Name)
			case p.Secret != nil:
				parts = append(parts, "Secret "+p.Secret.Name)
			case p.DownwardAPI != nil:
				parts = append(parts, "downward API")
			}
		}
		return text(strings.Join(parts, ", "))
	case s.CSI != nil:
		return text(s.CSI.Driver)
	case s.NFS != nil:
		return text(s.NFS.Server + ":" + s.NFS.Path)
	}
	return text("")
}

// claimTemplates lists a StatefulSet's volume claim templates. Each one
// becomes a claim per replica, named <template>-<statefulset>-<ordinal>.
func claimTemplates(sts *appsv1.StatefulSet) Table {
	t := Table{Title: "Volume claim templates", Columns: []string{"Template", "Size", "Access modes", "StorageClass"}}
	for _, tpl := range sts.Spec.VolumeClaimTemplates {
		size := ""
		if requested := tpl.Spec.Resources.Requests.Storage(); !requested.IsZero() {
			size = sizeText(requested.Value())
		}
		// Without a class, each claim gets the cluster's default one when it's made.
		class := text("(default)")
		if tpl.Spec.StorageClassName != nil && *tpl.Spec.StorageClassName != "" {
			class = Cell{Text: *tpl.Spec.StorageClassName, Link: classLink(*tpl.Spec.StorageClassName)}
		}
		t.addRow(text(tpl.Name), text(size), text(accessModeNames(tpl.Spec.AccessModes)), class)
	}
	return t
}

// ---- Related objects ----

// addRelated adds what can only be known by reading other objects: the
// pods using a claim, the claims made for a StatefulSet, and how much a
// StorageClass is used. If the user can't list those objects, the section
// is left out rather than failing the page.
func addRelated(ctx context.Context, clients Clients, d *Detail, resource string, obj *unstructured.Unstructured) error {
	var err error
	switch resource {
	case "persistentvolumeclaims":
		if err = checkClass(ctx, clients, d); err == nil {
			err = claimUsers(ctx, clients, d, obj)
		}
	case "persistentvolumes":
		err = checkClass(ctx, clients, d)
	case "statefulsets":
		err = statefulSetClaims(ctx, clients, d, obj)
	case "storageclasses":
		err = storageClassUsage(ctx, clients, d, obj)
	}
	if apierrors.IsForbidden(err) {
		return nil
	}
	return err
}

// checkClass makes sure the StorageClass a claim or volume names exists.
// A claim asking for a class that doesn't exist stays Pending for good, so
// that's worth saying, and a link to it would only lead to an error page.
// (Static volumes often name a class, like "manual", that is never created.)
func checkClass(ctx context.Context, clients Clients, d *Detail) error {
	for i := range d.Fields {
		f := &d.Fields[i]
		if f.Label != "StorageClass" || f.Link == nil {
			continue
		}
		_, err := clients.Kube.StorageV1().StorageClasses().Get(ctx, f.Link.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			f.Value += " (not found)"
			f.Link = nil
			return nil
		}
		return err
	}
	return nil
}

// claimUsers lists the pods that mount a claim. Pods can only use claims
// in their own namespace, so that's the only one searched.
func claimUsers(ctx context.Context, clients Clients, d *Detail, obj *unstructured.Unstructured) error {
	pods, err := clients.Kube.CoreV1().Pods(obj.GetNamespace()).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	t := Table{Title: "Used by", Columns: []string{"Pod", "Status", "Node"}}
	for _, pod := range pods.Items {
		for _, v := range pod.Spec.Volumes {
			if claimOf(&pod, v) != obj.GetName() {
				continue
			}
			link := &Link{Resource: "pods", Namespace: pod.Namespace, Name: pod.Name}
			t.addRow(Cell{Text: pod.Name, Link: link}, text(podStatus(&pod)), text(pod.Spec.NodeName))
			break
		}
	}
	d.addTable(t)
	return nil
}

// claimOf is the claim a pod volume uses, or "" for other kinds of volume.
func claimOf(pod *corev1.Pod, v corev1.Volume) string {
	switch {
	case v.PersistentVolumeClaim != nil:
		return v.PersistentVolumeClaim.ClaimName
	case v.Ephemeral != nil:
		return pod.Name + "-" + v.Name
	}
	return ""
}

// statefulSetClaims finds the claims made from a StatefulSet's templates.
// They're matched by name (<template>-<statefulset>-<ordinal>), which is
// how the StatefulSet controller itself finds them; they usually have no
// owner reference pointing back.
func statefulSetClaims(ctx context.Context, clients Clients, d *Detail, obj *unstructured.Unstructured) error {
	var sts appsv1.StatefulSet
	if err := convert(obj, &sts); err != nil {
		return err
	}
	if len(sts.Spec.VolumeClaimTemplates) == 0 {
		return nil
	}
	claims, err := clients.Kube.CoreV1().PersistentVolumeClaims(sts.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	t := Table{Title: "Claims", Columns: []string{"Claim", "Status", "Capacity", "Volume"}}
	sort.Slice(claims.Items, func(i, j int) bool { return claims.Items[i].Name < claims.Items[j].Name })
	for _, pvc := range claims.Items {
		if !madeFromTemplate(pvc.Name, &sts) {
			continue
		}
		capacity := ""
		if size, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			capacity = sizeText(size.Value())
		}
		volume := Cell{}
		if pvc.Spec.VolumeName != "" {
			volume = Cell{Text: pvc.Spec.VolumeName, Link: volumeLink(pvc.Spec.VolumeName)}
		}
		t.addRow(
			Cell{Text: pvc.Name, Link: claimLink(pvc.Namespace, pvc.Name)},
			text(storageStatus(pvc.ObjectMeta, string(pvc.Status.Phase))),
			text(capacity),
			volume,
		)
	}
	d.addTable(t)
	return nil
}

func madeFromTemplate(claim string, sts *appsv1.StatefulSet) bool {
	for _, tpl := range sts.Spec.VolumeClaimTemplates {
		ordinal, ok := strings.CutPrefix(claim, tpl.Name+"-"+sts.Name+"-")
		if _, err := strconv.Atoi(ordinal); ok && err == nil {
			return true
		}
	}
	return false
}

// storageClassUsage counts the volumes and claims of a StorageClass.
func storageClassUsage(ctx context.Context, clients Clients, d *Detail, obj *unstructured.Unstructured) error {
	volumes, err := clients.Kube.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	claims, err := clients.Kube.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	name := obj.GetName()
	inUse, claimed := 0, 0
	for _, pv := range volumes.Items {
		if volumeClass(&pv) == name {
			inUse++
		}
	}
	for _, pvc := range claims.Items {
		if claimClass(&pvc) == name {
			claimed++
		}
	}
	d.add("Volumes", strconv.Itoa(inUse))
	d.add("Claims", strconv.Itoa(claimed))
	return nil
}

// ---- Formatting ----

// accessModeNames spells access modes out: "ReadWriteOnce, ReadOnlyMany".
func accessModeNames(modes []corev1.PersistentVolumeAccessMode) string {
	names := []string{}
	for _, m := range modes {
		names = append(names, string(m))
	}
	return strings.Join(names, ", ")
}

// sizeText formats a size the way the tables do: "1 GiB", "1.5 GiB".
func sizeText(n int64) string {
	size := float64(n)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for size >= 1024 && i < len(units)-1 {
		size /= 1024
		i++
	}
	return strings.Replace(fmt.Sprintf("%.1f %s", size, units[i]), ".0 ", " ", 1)
}

// nodeSelectorText writes node affinity as one line:
// "kubernetes.io/hostname in (worker-1)". Terms are alternatives (or);
// the expressions within a term must all match (and).
func nodeSelectorText(s *corev1.NodeSelector) string {
	if s == nil {
		return ""
	}
	terms := []string{}
	for _, term := range s.NodeSelectorTerms {
		exprs := []string{}
		for _, e := range term.MatchExpressions {
			expr := e.Key + " " + strings.ToLower(string(e.Operator))
			if len(e.Values) > 0 {
				expr += " (" + strings.Join(e.Values, ", ") + ")"
			}
			exprs = append(exprs, expr)
		}
		terms = append(terms, strings.Join(exprs, " and "))
	}
	return strings.Join(terms, " or ")
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
