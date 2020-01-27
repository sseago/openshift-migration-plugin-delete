package migpvc

import (
	"encoding/json"

	"github.com/konveyor/openshift-migration-plugin/velero-plugins/migcommon"
	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	corev1API "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RestorePlugin is a restore item action plugin for Velero
type RestorePlugin struct {
	Log logrus.FieldLogger
}

// AppliesTo returns a velero.ResourceSelector that applies to PVCs
func (p *RestorePlugin) AppliesTo() (velero.ResourceSelector, error) {
	return velero.ResourceSelector{
		IncludedResources: []string{"persistentvolumeclaims"},
	}, nil
}

// Execute action for the restore plugin for the pvc resource
func (p *RestorePlugin) Execute(input *velero.RestoreItemActionExecuteInput) (*velero.RestoreItemActionExecuteOutput, error) {
	p.Log.Info("[pvc-restore] Entering Persistent Volume Claim restore plugin")

	pvc := corev1API.PersistentVolumeClaim{}
	itemMarshal, _ := json.Marshal(input.Item)
	json.Unmarshal(itemMarshal, &pvc)
	p.Log.Infof("[pvc-restore] pvc: %s", pvc.Name)

	// Use default behavior (restore the PV) for a swing migration.
	// For copy we remove annotations and PV volumeName
	if pvc.Annotations[migcommon.MigrateTypeAnnotation] == "copy" {

		// ISSUE-61 : removing the label selectors from PVC's
		// to avoid PV dynamic provisioner getting stuck
		pvc.Spec.Selector = nil
		storageClassName := pvc.Annotations[migcommon.MigrateStorageClassAnnotation]
		pvc.Spec.StorageClassName = &storageClassName
		if pvc.Annotations[corev1API.BetaStorageClassAnnotation] != "" {
			pvc.Annotations[corev1API.BetaStorageClassAnnotation] = storageClassName
		}
		accessMode := pvc.Annotations[migcommon.MigrateAccessModeAnnotation]
		if accessMode != "" {
			pvc.Spec.AccessModes = []corev1API.PersistentVolumeAccessMode{corev1API.PersistentVolumeAccessMode(accessMode)}
		}
	}
	delete(pvc.Annotations, migcommon.PVCSelectedNodeAnnotation)

	var out map[string]interface{}
	objrec, _ := json.Marshal(pvc)
	json.Unmarshal(objrec, &out)

	return velero.NewRestoreItemActionExecuteOutput(&unstructured.Unstructured{Object: out}), nil
}
