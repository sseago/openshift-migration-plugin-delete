package migpod

import (
	"encoding/json"
	"fmt"

	"github.com/konveyor/openshift-migration-plugin/velero-plugins/migcommon"
	"github.com/konveyor/openshift-velero-plugin/velero-plugins/common"
	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	corev1API "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RestorePlugin is a restore item action plugin for Velero
type RestorePlugin struct {
	Log logrus.FieldLogger
}

// AppliesTo returns a velero.ResourceSelector that applies to pods
func (p *RestorePlugin) AppliesTo() (velero.ResourceSelector, error) {
	return velero.ResourceSelector{
		IncludedResources: []string{"pods"},
	}, nil
}

// Execute action for the restore plugin for the pod resource
func (p *RestorePlugin) Execute(input *velero.RestoreItemActionExecuteInput) (*velero.RestoreItemActionExecuteOutput, error) {
	p.Log.Info("[pod-restore] Entering Pod restore plugin")

	pod := corev1API.Pod{}
	itemMarshal, _ := json.Marshal(input.Item)
	json.Unmarshal(itemMarshal, &pod)
	p.Log.Infof("[pod-restore] pod: %s", pod.Name)

	// ISSUE-61 : removing the node selectors from pods
	// to avoid pod being `unschedulable` on destination
	pod.Spec.NodeSelector = nil

	if input.Restore.Annotations[migcommon.MigrateCopyPhaseAnnotation] == "stage" {
		migcommon.ConfigureContainerSleep(pod.Spec.Containers, "infinity")
		migcommon.ConfigureContainerSleep(pod.Spec.InitContainers, "0")
		pod.Labels[migcommon.PodStageLabel] = "true"
		pod.Spec.Affinity = nil
	} else {
		registry := pod.Annotations[common.RestoreRegistryHostname]
		backupRegistry := pod.Annotations[common.BackupRegistryHostname]
		if registry == "" {
			return nil, fmt.Errorf("failed to find restore registry annotation")
		}
		common.SwapContainerImageRefs(pod.Spec.Containers, backupRegistry, registry, p.Log, input.Restore.Spec.NamespaceMapping)
		common.SwapContainerImageRefs(pod.Spec.InitContainers, backupRegistry, registry, p.Log, input.Restore.Spec.NamespaceMapping)

		ownerRefs, err := common.GetOwnerReferences(input.ItemFromBackup)
		if err != nil {
			return nil, err
		}
		// Check if pod has owner Refs and does not have restic backup associated with it
		if len(ownerRefs) > 0 && pod.Annotations[migcommon.ResticBackupAnnotation] == "" {
			p.Log.Infof("[pod-restore] skipping restore of pod %s, has owner references and no restic backup", pod.Name)
			return velero.NewRestoreItemActionExecuteOutput(input.Item).WithoutRestore(), nil
		}
	}

	var out map[string]interface{}
	objrec, _ := json.Marshal(pod)
	json.Unmarshal(objrec, &out)

	return velero.NewRestoreItemActionExecuteOutput(&unstructured.Unstructured{Object: out}), nil
}
