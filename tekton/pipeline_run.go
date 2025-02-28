/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tekton

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ecapiv1alpha1 "github.com/enterprise-contract/enterprise-contract-controller/api/v1alpha1"
	"github.com/redhat-appstudio/release-service/metadata"

	libhandler "github.com/operator-framework/operator-lib/handler"
	integrationServiceGitopsPkg "github.com/redhat-appstudio/integration-service/gitops"
	"github.com/redhat-appstudio/release-service/api/v1alpha1"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PipelineType represents a PipelineRun type within AppStudio
type PipelineType string

const (
	// PipelineTypeRelease is the type for PipelineRuns created to run a release Pipeline
	PipelineTypeRelease = "release"
)

// ReleasePipelineRun is a PipelineRun alias, so we can add new methods to it in this file.
type ReleasePipelineRun struct {
	tektonv1.PipelineRun
}

// NewReleasePipelineRun creates an empty PipelineRun in the given namespace. The name will be autogenerated,
// using the prefix passed as an argument to the function.
func NewReleasePipelineRun(prefix, namespace string) *ReleasePipelineRun {
	pipelineRun := tektonv1.PipelineRun{
		ObjectMeta: v1.ObjectMeta{
			GenerateName: prefix + "-",
			Namespace:    namespace,
		},
		Spec: tektonv1.PipelineRunSpec{},
	}

	return &ReleasePipelineRun{pipelineRun}
}

// AsPipelineRun casts the ReleasePipelineRun to PipelineRun, so it can be used in the Kubernetes client.
func (r *ReleasePipelineRun) AsPipelineRun() *tektonv1.PipelineRun {
	return &r.PipelineRun
}

// WithEnterpriseContractConfigMap adds a param providing the verify ec task git resolver information to the release PipelineRun.
func (r *ReleasePipelineRun) WithEnterpriseContractConfigMap(ecConfig *corev1.ConfigMap) *ReleasePipelineRun {
	gitResolverFields := []string{"verify_ec_task_git_url", "verify_ec_task_git_revision", "verify_ec_task_git_pathInRepo"}

	for _, field := range gitResolverFields {
		r.WithExtraParam(field, tektonv1.ParamValue{
			Type:      tektonv1.ParamTypeString,
			StringVal: ecConfig.Data[string(field)],
		})
	}

	return r
}

// WithEnterpriseContractPolicy adds a param containing the EnterpriseContractPolicy Spec as a json string to the release PipelineRun.
func (r *ReleasePipelineRun) WithEnterpriseContractPolicy(enterpriseContractPolicy *ecapiv1alpha1.EnterpriseContractPolicy) *ReleasePipelineRun {
	policyJson, _ := json.Marshal(enterpriseContractPolicy.Spec)

	policyKindRunes := []rune(enterpriseContractPolicy.Kind)
	policyKindRunes[0] = unicode.ToLower(policyKindRunes[0])

	r.WithExtraParam(string(policyKindRunes), tektonv1.ParamValue{
		Type:      tektonv1.ParamTypeString,
		StringVal: string(policyJson),
	})

	return r
}

// WithExtraParam adds an extra param to the release PipelineRun. If the parameter is not part of the Pipeline
// definition, it will be silently ignored.
func (r *ReleasePipelineRun) WithExtraParam(name string, value tektonv1.ParamValue) *ReleasePipelineRun {
	r.Spec.Params = append(r.Spec.Params, tektonv1.Param{
		Name:  name,
		Value: value,
	})

	return r
}

// WithObjectReferences adds new parameters to the PipelineRun for each object passed as an argument to the function.
// The new parameters will be named after the kind of the object and its values will be a reference to the object itself
// in the form of "namespace/name".
func (r *ReleasePipelineRun) WithObjectReferences(objects ...client.Object) *ReleasePipelineRun {
	for _, object := range objects {
		r.WithExtraParam(strings.ToLower(object.GetObjectKind().GroupVersionKind().Kind), tektonv1.ParamValue{
			Type:      tektonv1.ParamTypeString,
			StringVal: fmt.Sprintf("%s%c%s", object.GetNamespace(), types.Separator, object.GetName()),
		})
	}

	return r
}

// WithOwner sets owner annotations to the release PipelineRun and a finalizer to prevent its deletion.
func (r *ReleasePipelineRun) WithOwner(release *v1alpha1.Release) *ReleasePipelineRun {
	_ = libhandler.SetOwnerAnnotations(release, r)
	controllerutil.AddFinalizer(r, metadata.ReleaseFinalizer)

	return r
}

// WithPipelineRef sets the PipelineRef for the release PipelineRun.
func (r *ReleasePipelineRun) WithPipelineRef(pipelineRef *tektonv1.PipelineRef) *ReleasePipelineRun {
	r.Spec.PipelineRef = pipelineRef

	return r
}

// WithReleaseAndApplicationMetadata adds Release and Application metadata to the release PipelineRun.
func (r *ReleasePipelineRun) WithReleaseAndApplicationMetadata(release *v1alpha1.Release, applicationName string) *ReleasePipelineRun {
	r.ObjectMeta.Labels = map[string]string{
		metadata.PipelinesTypeLabel:    PipelineTypeRelease,
		metadata.ReleaseNameLabel:      release.Name,
		metadata.ReleaseNamespaceLabel: release.Namespace,
		metadata.ApplicationNameLabel:  applicationName,
	}
	metadata.AddAnnotations(r.AsPipelineRun(), metadata.GetAnnotationsWithPrefix(release, integrationServiceGitopsPkg.PipelinesAsCodePrefix))
	metadata.AddLabels(r.AsPipelineRun(), metadata.GetLabelsWithPrefix(release, integrationServiceGitopsPkg.PipelinesAsCodePrefix))

	return r
}

// WithServiceAccount adds a reference to the service account to be used to gain elevated privileges during the
// execution of the different Pipeline tasks.
func (r *ReleasePipelineRun) WithServiceAccount(serviceAccount string) *ReleasePipelineRun {
	r.Spec.TaskRunTemplate.ServiceAccountName = serviceAccount

	return r
}

// WithWorkspace adds a workspace to the PipelineRun using the given name and PersistentVolumeClaim.
// If any of those values is empty, no workspace will be added.
func (r *ReleasePipelineRun) WithWorkspace(name, persistentVolumeClaim string) *ReleasePipelineRun {
	if name == "" || persistentVolumeClaim == "" {
		return r
	}

	r.Spec.Workspaces = append(r.Spec.Workspaces, tektonv1.WorkspaceBinding{
		Name: name,
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: persistentVolumeClaim,
		},
	})

	return r
}
