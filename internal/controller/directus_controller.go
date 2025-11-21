/*
Copyright 2025.

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

package controller

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	appsv1alpha1 "github.com/zjpiazza/directus-operator/api/v1alpha1"
)

// DirectusReconciler reconciles a Directus object
type DirectusReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apps.directus.io,resources=directuses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.directus.io,resources=directuses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.directus.io,resources=directuses/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DirectusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the Directus instance
	directus := &appsv1alpha1.Directus{}
	err := r.Get(ctx, req.NamespacedName, directus)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Reconcile Database
	dbSecretName := ""
	if directus.Spec.Database.Type == "postgres" && directus.Spec.Database.Postgres != nil {
		dbSecretName, err = r.reconcilePostgres(ctx, directus)
		if err != nil {
			log.Error(err, "Failed to reconcile Postgres")
			return ctrl.Result{}, err
		}

		// Check if Database is ready
		clusterName := directus.Name + "-db"
		cluster := &cnpgv1.Cluster{}
		err = r.Get(ctx, client.ObjectKey{Name: clusterName, Namespace: directus.Namespace}, cluster)
		if err != nil {
			if errors.IsNotFound(err) {
				// Cluster not created yet, requeue
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "Failed to get Postgres Cluster")
			return ctrl.Result{}, err
		}

		if cluster.Status.Phase != "Cluster in healthy state" {
			log.Info("Postgres Cluster is not ready yet", "Phase", cluster.Status.Phase)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Reconcile Deployment
	err = r.reconcileDeployment(ctx, directus, dbSecretName)
	if err != nil {
		log.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// Reconcile Service
	err = r.reconcileService(ctx, directus)
	if err != nil {
		log.Error(err, "Failed to reconcile Service")
		return ctrl.Result{}, err
	}

	// Reconcile Ingress
	if directus.Spec.Ingress.Enabled {
		err = r.reconcileIngress(ctx, directus)
		if err != nil {
			log.Error(err, "Failed to reconcile Ingress")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *DirectusReconciler) reconcilePostgres(ctx context.Context, directus *appsv1alpha1.Directus) (string, error) {
	clusterName := directus.Name + "-db"
	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: directus.Namespace,
		},
	}

	_, err := ctrl.CreateOrUpdate(ctx, r.Client, cluster, func() error {
		cluster.Spec.Instances = directus.Spec.Database.Postgres.Instances
		cluster.Spec.StorageConfiguration.Size = directus.Spec.Database.Postgres.Storage

		// Basic configuration for CNPG
		if cluster.Spec.Bootstrap == nil {
			cluster.Spec.Bootstrap = &cnpgv1.BootstrapConfiguration{
				InitDB: &cnpgv1.BootstrapInitDB{
					Database: "directus",
					Owner:    "directus",
				},
			}
		}
		return ctrl.SetControllerReference(directus, cluster, r.Scheme)
	})

	if err != nil {
		return "", err
	}

	// Return the secret name that CNPG creates (usually clusterName-app)
	return clusterName + "-app", nil
}

func (r *DirectusReconciler) reconcileDeployment(ctx context.Context, directus *appsv1alpha1.Directus, dbSecretName string) error {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      directus.Name,
			Namespace: directus.Namespace,
		},
	}

	_, err := ctrl.CreateOrUpdate(ctx, r.Client, dep, func() error {
		replicas := int32(1)
		if directus.Spec.Replicas != nil {
			replicas = *directus.Spec.Replicas
		}
		dep.Spec.Replicas = &replicas
		dep.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": directus.Name},
		}
		dep.Spec.Template.ObjectMeta.Labels = map[string]string{"app": directus.Name}

		// Init Container for Extensions
		initContainers := []corev1.Container{}
		volumeMounts := []corev1.VolumeMount{}
		volumes := []corev1.Volume{}

		if len(directus.Spec.Extensions) > 0 {
			// Create a volume for extensions
			volumes = append(volumes, corev1.Volume{
				Name: "extensions",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "extensions",
				MountPath: "/directus/extensions",
			})

			// Init container to download extensions
			// We use node:20-alpine to support npm installs and git
			cmd := "apk add --no-cache git && mkdir -p /temp-extensions && cd /temp-extensions && "

			initContainerEnv := []corev1.EnvVar{}
			initContainerVolumeMounts := []corev1.VolumeMount{
				{
					Name:      "extensions",
					MountPath: "/directus/extensions",
				},
			}

			if directus.Spec.ExtensionsConfig != nil && directus.Spec.ExtensionsConfig.SecretRef != "" {
				volumes = append(volumes, corev1.Volume{
					Name: "npmrc",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: directus.Spec.ExtensionsConfig.SecretRef,
						},
					},
				})
				initContainerVolumeMounts = append(initContainerVolumeMounts, corev1.VolumeMount{
					Name:      "npmrc",
					MountPath: "/root/.npmrc",
					SubPath:   ".npmrc",
				})
			}

			for _, ext := range directus.Spec.Extensions {
				cmd += fmt.Sprintf("echo 'Processing extension %s'; ", ext.Name)
				if ext.Type == "npm" {
					// Install via npm
					installCmd := ext.Source
					if ext.Version != "" {
						installCmd = fmt.Sprintf("%s@%s", ext.Source, ext.Version)
					}
					cmd += fmt.Sprintf("npm install %s && ", installCmd)

					// Extract package name from source to handle versions (e.g. pkg@1.0.0)
					pkgName := ext.Source
					if strings.HasPrefix(pkgName, "@") {
						if idx := strings.Index(pkgName[1:], "@"); idx != -1 {
							pkgName = pkgName[:idx+1]
						}
					} else {
						if idx := strings.Index(pkgName, "@"); idx != -1 {
							pkgName = pkgName[:idx]
						}
					}

					// Move from node_modules to the target directory
					// We assume Source is the package name.
					cmd += fmt.Sprintf("mkdir -p /directus/extensions/%s && cp -r node_modules/%s/* /directus/extensions/%s/; ", ext.Name, pkgName, ext.Name)
				} else if ext.Type == "git" {
					// Git clone
					cmd += fmt.Sprintf("mkdir -p /directus/extensions/%s && ", ext.Name)
					cmd += fmt.Sprintf("git clone %s /directus/extensions/%s; ", ext.Source, ext.Name)
				} else {
					// Default to URL/tarball
					cmd += fmt.Sprintf("mkdir -p /directus/extensions/%s && ", ext.Name)
					cmd += fmt.Sprintf("wget -O /temp-extensions/%s.tar.gz %s && tar -xzf /temp-extensions/%s.tar.gz -C /directus/extensions/%s; ", ext.Name, ext.Source, ext.Name, ext.Name)
				}
			}

			initContainers = append(initContainers, corev1.Container{
				Name:         "install-extensions",
				Image:        "node:20-alpine",
				Command:      []string{"sh", "-c", cmd},
				VolumeMounts: initContainerVolumeMounts,
				Env:          initContainerEnv,
			})
		}

		dep.Spec.Template.Spec.InitContainers = initContainers
		dep.Spec.Template.Spec.Volumes = volumes

		// Main Container
		container := corev1.Container{
			Name:  "directus",
			Image: directus.Spec.Image,
			Ports: []corev1.ContainerPort{{ContainerPort: 8055}},
			Env: []corev1.EnvVar{
				{Name: "KEY", Value: "directus-key"},       // Should be a secret
				{Name: "SECRET", Value: "directus-secret"}, // Should be a secret
				{Name: "ADMIN_EMAIL", Value: "admin@example.com"},
				{Name: "ADMIN_PASSWORD", Value: "password"},
			},
			VolumeMounts: volumeMounts,
		}

		if directus.Spec.PublicURL != "" {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "PUBLIC_URL",
				Value: directus.Spec.PublicURL,
			})
		} else if directus.Spec.Ingress.Enabled && directus.Spec.Ingress.Host != "" {
			protocol := "http"
			if directus.Spec.Ingress.TLS {
				protocol = "https"
			}
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "PUBLIC_URL",
				Value: fmt.Sprintf("%s://%s", protocol, directus.Spec.Ingress.Host),
			})
		}

		if dbSecretName != "" {
			container.Env = append(container.Env,
				corev1.EnvVar{
					Name:  "DB_CLIENT",
					Value: "postgresql",
				},
				corev1.EnvVar{
					Name: "DB_HOST",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: dbSecretName},
							Key:                  "host",
						},
					},
				},
				corev1.EnvVar{
					Name:  "DB_PORT",
					Value: "5432",
				},
				corev1.EnvVar{
					Name: "DB_DATABASE",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: dbSecretName},
							Key:                  "dbname",
						},
					},
				},
				corev1.EnvVar{
					Name: "DB_USER",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: dbSecretName},
							Key:                  "user",
						},
					},
				},
				corev1.EnvVar{
					Name: "DB_PASSWORD",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: dbSecretName},
							Key:                  "password",
						},
					},
				},
			)
		}

		dep.Spec.Template.Spec.Containers = []corev1.Container{container}
		return ctrl.SetControllerReference(directus, dep, r.Scheme)
	})

	return err
}

func (r *DirectusReconciler) reconcileService(ctx context.Context, directus *appsv1alpha1.Directus) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      directus.Name,
			Namespace: directus.Namespace,
		},
	}

	_, err := ctrl.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Spec.Selector = map[string]string{"app": directus.Name}
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Port:       8055,
				TargetPort: intstr.FromInt(8055),
				Protocol:   corev1.ProtocolTCP,
			},
		}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		return ctrl.SetControllerReference(directus, svc, r.Scheme)
	})

	return err
}

func (r *DirectusReconciler) reconcileIngress(ctx context.Context, directus *appsv1alpha1.Directus) error {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      directus.Name,
			Namespace: directus.Namespace,
		},
	}

	_, err := ctrl.CreateOrUpdate(ctx, r.Client, ing, func() error {
		pathType := networkingv1.PathTypePrefix
		ing.Spec.Rules = []networkingv1.IngressRule{
			{
				Host: directus.Spec.Ingress.Host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &pathType,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: directus.Name,
										Port: networkingv1.ServiceBackendPort{
											Number: 8055,
										},
									},
								},
							},
						},
					},
				},
			},
		}
		return ctrl.SetControllerReference(directus, ing, r.Scheme)
	})

	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *DirectusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.Directus{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&cnpgv1.Cluster{}).
		Complete(r)
}
