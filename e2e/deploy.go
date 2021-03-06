/*
Copyright 2020 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"github.com/GoogleCloudPlatform/gke-managed-certs/e2e/utils"
	utilshttp "github.com/GoogleCloudPlatform/gke-managed-certs/pkg/utils/http"
)

const (
	clusterRoleBindingName = "managed-certificate-role-binding"
	clusterRoleName        = "managed-certificate-role"
	deploymentName         = "managed-certificate-controller"
	serviceAccountName     = "managed-certificate-account"
)

// Deploys Managed Certificate CRD
func deployCRD() error {
	domainRegex := `^(([a-zA-Z0-9]+|[a-zA-Z0-9][-a-zA-Z0-9]*[a-zA-Z0-9])\.)+[a-zA-Z][-a-zA-Z0-9]*[a-zA-Z0-9]\.?$`
	var maxDomains1 int64 = 1
	var maxDomains100 int64 = 100
	var maxDomainLength int64 = 63
	crd := apiextv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "managedcertificates.networking.gke.io",
		},
		Spec: apiextv1beta1.CustomResourceDefinitionSpec{
			Group: "networking.gke.io",
			Versions: []apiextv1beta1.CustomResourceDefinitionVersion{
				{
					Name:    "v1beta1",
					Served:  true,
					Storage: false,
					Schema: &apiextv1beta1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextv1beta1.JSONSchemaProps{
							Properties: map[string]apiextv1beta1.JSONSchemaProps{
								"status": {
									Properties: map[string]apiextv1beta1.JSONSchemaProps{
										"certificateStatus": {Type: "string"},
										"domainStatus": {
											Type: "array",
											Items: &apiextv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextv1beta1.JSONSchemaProps{
													Type:     "object",
													Required: []string{"domain", "status"},
													Properties: map[string]apiextv1beta1.JSONSchemaProps{
														"domain": {Type: "string"},
														"status": {Type: "string"},
													},
												},
											},
										},
										"certificateName": {Type: "string"},
										"expireTime":      {Type: "string", Format: "date-time"},
									},
								},
								"spec": {
									Properties: map[string]apiextv1beta1.JSONSchemaProps{
										"domains": {
											Type:     "array",
											MaxItems: &maxDomains1,
											Items: &apiextv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextv1beta1.JSONSchemaProps{
													Type:      "string",
													MaxLength: &maxDomainLength,
													Pattern:   domainRegex,
												},
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Name:    "v1beta2",
					Served:  true,
					Storage: true,
					Schema: &apiextv1beta1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextv1beta1.JSONSchemaProps{
							Properties: map[string]apiextv1beta1.JSONSchemaProps{
								"status": {
									Properties: map[string]apiextv1beta1.JSONSchemaProps{
										"certificateStatus": {Type: "string"},
										"domainStatus": {
											Type: "array",
											Items: &apiextv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextv1beta1.JSONSchemaProps{
													Type:     "object",
													Required: []string{"domain", "status"},
													Properties: map[string]apiextv1beta1.JSONSchemaProps{
														"domain": {Type: "string"},
														"status": {Type: "string"},
													},
												},
											},
										},
										"certificateName": {Type: "string"},
										"expireTime":      {Type: "string", Format: "date-time"},
									},
								},
								"spec": {
									Properties: map[string]apiextv1beta1.JSONSchemaProps{
										"domains": {
											Type:     "array",
											MaxItems: &maxDomains100,
											Items: &apiextv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextv1beta1.JSONSchemaProps{
													Type:      "string",
													MaxLength: &maxDomainLength,
													Pattern:   domainRegex,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			Names: apiextv1beta1.CustomResourceDefinitionNames{
				Plural:     "managedcertificates",
				Singular:   "managedcertificate",
				Kind:       "ManagedCertificate",
				ShortNames: []string{"mcrt"},
			},
			Scope: apiextv1beta1.NamespaceScoped,
		},
	}
	if err := utilshttp.IgnoreNotFound(clients.CustomResource.Delete(crd.Name, &metav1.DeleteOptions{})); err != nil {
		return err
	}
	if _, err := clients.CustomResource.Create(&crd); err != nil {
		return err
	}
	klog.Infof("Created custom resource definition %s", crd.Name)

	if err := utils.Retry(func() error {
		crd, err := clients.CustomResource.Get(crd.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("ManagedCertificate CRD not yet established: %v", err)
		}

		for _, c := range crd.Status.Conditions {
			if c.Type == apiextv1beta1.Established && c.Status == apiextv1beta1.ConditionTrue {
				return nil
			}
		}

		return errors.New("ManagedCertificate CRD not yet established")
	}); err != nil {
		return err
	}

	return nil
}

// Deploys Managed Certificate controller with all related objects
func deployController(tag string) error {
	if err := deleteController(); err != nil {
		return err
	}

	serviceAccount := corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName}}
	if _, err := clients.ServiceAccount.Create(&serviceAccount); err != nil {
		return err
	}
	klog.Infof("Created service account %s", serviceAccountName)

	clusterRole := rbacv1beta1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName},
		Rules: []rbacv1beta1.PolicyRule{
			{
				APIGroups: []string{"networking.gke.io"},
				Resources: []string{"managedcertificates"},
				Verbs:     []string{"*"},
			},
			{
				APIGroups: []string{"", "extensions"},
				Resources: []string{"configmaps", "endpoints", "events", "ingresses"},
				Verbs:     []string{"*"},
			},
		},
	}
	if _, err := clients.ClusterRole.Create(&clusterRole); err != nil {
		return err
	}
	klog.Infof("Created cluster role %s", clusterRoleName)

	clusterRoleBinding := rbacv1beta1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleBindingName},
		Subjects:   []rbacv1beta1.Subject{{Namespace: "default", Name: serviceAccountName, Kind: "ServiceAccount"}},
		RoleRef: rbacv1beta1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
	}
	if _, err := clients.ClusterRoleBinding.Create(&clusterRoleBinding); err != nil {
		return err
	}
	klog.Infof("Created cluster role binding %s", clusterRoleBindingName)

	appCtrl := map[string]string{"app": deploymentName}
	image := fmt.Sprintf("eu.gcr.io/managed-certs-gke/managed-certificate-controller:%s", tag)
	fileOrCreate := corev1.HostPathFileOrCreate

	sslCertsVolume := "ssl-certs"
	sslCertsVolumePath := "/etc/ssl/certs"

	usrShareCaCertsVolume := "usrsharecacerts"
	usrShareCaCertsVolumePath := "/usr/share/ca-certificates"

	logFileVolume := "logfile"
	logFileVolumePath := "/var/log/managed_certificate_controller.log"

	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: deploymentName},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: appCtrl},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: appCtrl},
				Spec: corev1.PodSpec{
					ServiceAccountName: serviceAccountName,
					RestartPolicy:      corev1.RestartPolicyAlways,
					Containers: []corev1.Container{
						{
							Name:            deploymentName,
							Image:           image,
							ImagePullPolicy: corev1.PullAlways,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      sslCertsVolume,
									MountPath: sslCertsVolumePath,
									ReadOnly:  true,
								},
								{
									Name:      usrShareCaCertsVolume,
									MountPath: usrShareCaCertsVolumePath,
									ReadOnly:  true,
								},
								{
									Name:      logFileVolume,
									MountPath: logFileVolumePath,
									ReadOnly:  false,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: sslCertsVolume,
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: sslCertsVolumePath,
								},
							},
						},
						{
							Name: usrShareCaCertsVolume,
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: usrShareCaCertsVolumePath,
								},
							},
						},
						{
							Name: logFileVolume,
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: logFileVolumePath,
									Type: &fileOrCreate,
								},
							},
						},
					},
				},
			},
		},
	}
	if _, err := clients.Deployment.Create(&deployment); err != nil {
		return err
	}
	klog.Infof("Created deployment %s", deploymentName)

	return nil
}

// Deletes Managed Certificate controller and all related objects
func deleteController() error {
	if err := utilshttp.IgnoreNotFound(clients.ServiceAccount.Delete(serviceAccountName, &metav1.DeleteOptions{})); err != nil {
		return err
	}
	klog.Infof("Deleted service account %s", serviceAccountName)

	if err := utilshttp.IgnoreNotFound(clients.ClusterRole.Delete(clusterRoleName, &metav1.DeleteOptions{})); err != nil {
		return err
	}
	klog.Infof("Deleted cluster role %s", clusterRoleName)

	if err := utilshttp.IgnoreNotFound(clients.ClusterRoleBinding.Delete(clusterRoleBindingName, &metav1.DeleteOptions{})); err != nil {
		return err
	}
	klog.Infof("Deleted cluster role binding %s", clusterRoleBindingName)

	if err := utilshttp.IgnoreNotFound(clients.Deployment.Delete(deploymentName, &metav1.DeleteOptions{})); err != nil {
		return err
	}
	klog.Infof("Deleted deployment %s", deploymentName)

	return nil
}
