package migimagestream

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/konveyor/openshift-migration-plugin/velero-plugins/migcommon"
	"github.com/konveyor/openshift-velero-plugin/velero-plugins/common"
	imagev1API "github.com/openshift/api/image/v1"
	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
)

// MyRestorePlugin is a restore item action plugin for Velero
type RestorePlugin struct {
	Log logrus.FieldLogger
}

// AppliesTo returns a velero.ResourceSelector that applies to everything
func (p *RestorePlugin) AppliesTo() (velero.ResourceSelector, error) {
	return velero.ResourceSelector{
		IncludedResources: []string{"imagestreams"},
	}, nil
}

// Execute copies local registry images from migration registry into target cluster local registry
func (p *RestorePlugin) Execute(input *velero.RestoreItemActionExecuteInput) (*velero.RestoreItemActionExecuteOutput, error) {
	p.Log.Info("[is-restore] Entering ImageStream restore plugin")

	imageStream := imagev1API.ImageStream{}
	itemMarshal, _ := json.Marshal(input.Item)
	json.Unmarshal(itemMarshal, &imageStream)
	p.Log.Info(fmt.Sprintf("[is-restore] image: %#v", imageStream.Name))
	annotations := imageStream.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}

	imageStreamUnmodified := imagev1API.ImageStream{}
	itemMarshal, _ = json.Marshal(input.ItemFromBackup)
	json.Unmarshal(itemMarshal, &imageStreamUnmodified)

	internalRegistry := annotations[common.RestoreRegistryHostname]
	if len(internalRegistry) == 0 {
		return nil, errors.New("restore cluster registry not found for annotation \"openshift.io/restore-registry-hostname\"")
	}
	backupInternalRegistry := annotations[common.BackupRegistryHostname]
	if len(internalRegistry) == 0 {
		return nil, errors.New("backup cluster registry not found for annotation \"openshift.io/backup-registry-hostname\"")
	}
	migrationRegistry := input.Restore.Annotations[migcommon.MigrationRegistry]
	if len(migrationRegistry) == 0 {
		return nil, errors.New("migration registry not found for annotation \"openshift.io/migration\"")
	}
	p.Log.Info(fmt.Sprintf("[is-restore] backup internal registry: %#v", backupInternalRegistry))
	p.Log.Info(fmt.Sprintf("[is-restore] restore internal registry: %#v", internalRegistry))

	for _, tag := range imageStreamUnmodified.Status.Tags {
		p.Log.Info(fmt.Sprintf("[is-restore] Restoring tag: %#v", tag.Tag))
		specTag := findSpecTag(imageStreamUnmodified.Spec.Tags, tag.Tag)
		copyToTag := true
		if specTag != nil && specTag.From != nil {
			p.Log.Info(fmt.Sprintf("[is-restore] image tagged: %s, %s", specTag.From.Kind, specTag.From.Name))
			// we have a tag.
			copyToTag = false
		}
		// Iterate over items in reverse order so most recently tagged is copied last
		for i := len(tag.Items) - 1; i >= 0; i-- {
			dockerImageReference := tag.Items[i].DockerImageReference
			if strings.HasPrefix(dockerImageReference, backupInternalRegistry) {
				destTag := ""
				if copyToTag {
					destTag = ":" + tag.Tag
				}

				destNamespace := imageStreamUnmodified.Namespace

				// if destination namespace is mapped to new one, swap it
				namespaceMapping := input.Restore.Spec.NamespaceMapping
				if namespaceMapping[destNamespace] != "" {
					destNamespace = namespaceMapping[imageStreamUnmodified.Namespace]
				}

				srcPath := fmt.Sprintf("docker://%s/%s/%s@%s", migrationRegistry, imageStreamUnmodified.Namespace, imageStreamUnmodified.Name, tag.Items[i].Image)
				destPath := fmt.Sprintf("docker://%s/%s/%s%s", internalRegistry, destNamespace, imageStreamUnmodified.Name, destTag)

				p.Log.Info(fmt.Sprintf("[is-restore] copying from: %s", srcPath))
				p.Log.Info(fmt.Sprintf("[is-restore] copying to: %s", destPath))
				manifest, err := copyImageRestore(srcPath, destPath)
				if err != nil {
					p.Log.Info(fmt.Sprintf("[is-restore] Error copying image: %v", err))
					return nil, err
				}
				p.Log.Info(fmt.Sprintf("[is-restore] manifest of copied image: %s", manifest))
			}
		}
	}
	var out map[string]interface{}
	objrec, _ := json.Marshal(imageStream)
	json.Unmarshal(objrec, &out)
	input.Item.SetUnstructuredContent(out)

	return velero.NewRestoreItemActionExecuteOutput(input.Item).WithoutRestore(), nil
}

func copyImageRestore(src, dest string) ([]byte, error) {
	sourceCtx, err := migrationRegistrySystemContext()
	if err != nil {
		return []byte{}, err
	}
	destinationCtx, err := internalRegistrySystemContext()
	if err != nil {
		return []byte{}, err
	}
	return copyImage(src, dest, sourceCtx, destinationCtx)
}
