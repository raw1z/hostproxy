/*
Copyright 2024.

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
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1 "github.com/raw1z/hostproxy/api/v1"
)

const hostproxyFinalizer = "networking.raw1z.fr/finalizer"

// Definitions to manage status conditions
const (
	// typeAvailableHostproxy represents the status of the Deployment reconciliation
	typeAvailableHostproxy = "Available"
	// typeDegradedHostproxy represents the status used when the custom resource is deleted and the finalizer operations are must to occur.
	typeDegradedHostproxy = "Degraded"
)

// HostproxyReconciler reconciles a Hostproxy object
type HostproxyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html

//+kubebuilder:rbac:groups=networking.raw1z.fr,resources=hostproxies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.raw1z.fr,resources=hostproxies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.raw1z.fr,resources=hostproxies/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=list;watch;get;patch;create;update
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It is essential for the controller's reconciliation loop to be idempotent. By following the Operator
// pattern you will create Controllers which provide a reconcile function
// responsible for synchronizing resources until the desired state is reached on the cluster.
// Breaking this recommendation goes against the design principles of controller-runtime.
// and may lead to unforeseen consequences such as resources becoming stuck and requiring manual intervention.
// For further info:
// - About Operator Pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - About Controllers: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *HostproxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the Hostproxy instance
	// The purpose is check if the Custom Resource for the Kind Hostproxy
	// is applied on the cluster if not we return nil to stop the reconciliation
	hostproxy := &networkingv1.Hostproxy{}
	err := r.Get(ctx, req.NamespacedName, hostproxy)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then, it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("hostproxy resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get hostproxy")
		return ctrl.Result{}, err
	}

	// Let's just set the status as Unknown when no status are available
	if hostproxy.Status.Conditions == nil || len(hostproxy.Status.Conditions) == 0 {
		meta.SetStatusCondition(&hostproxy.Status.Conditions, metav1.Condition{Type: typeAvailableHostproxy, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, hostproxy); err != nil {
			log.Error(err, "Failed to update Hostproxy status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the hostproxy Custom Resource after update the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raise the issue "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, hostproxy); err != nil {
			log.Error(err, "Failed to re-fetch hostproxy")
			return ctrl.Result{}, err
		}
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occurs before the custom resource to be deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(hostproxy, hostproxyFinalizer) {
		log.Info("Adding Finalizer for Hostproxy")
		if ok := controllerutil.AddFinalizer(hostproxy, hostproxyFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, hostproxy); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the Hostproxy instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isHostproxyMarkedToBeDeleted := hostproxy.GetDeletionTimestamp() != nil
	if isHostproxyMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(hostproxy, hostproxyFinalizer) {
			log.Info("Performing Finalizer Operations for Hostproxy before delete CR")

			// Let's add here an status "Downgrade" to define that this resource begin its process to be terminated.
			meta.SetStatusCondition(&hostproxy.Status.Conditions, metav1.Condition{Type: typeDegradedHostproxy,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", hostproxy.Name)})

			if err := r.Status().Update(ctx, hostproxy); err != nil {
				log.Error(err, "Failed to update Hostproxy status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before remove the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			r.doFinalizerOperationsForHostproxy(hostproxy)

			// TODO(user): If you add operations to the doFinalizerOperationsForHostproxy method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			// Re-fetch the hostproxy Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, hostproxy); err != nil {
				log.Error(err, "Failed to re-fetch hostproxy")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&hostproxy.Status.Conditions, metav1.Condition{Type: typeDegradedHostproxy,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", hostproxy.Name)})

			if err := r.Status().Update(ctx, hostproxy); err != nil {
				log.Error(err, "Failed to update Hostproxy status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for Hostproxy after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(hostproxy, hostproxyFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for Hostproxy")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, hostproxy); err != nil {
				log.Error(err, "Failed to remove finalizer for Hostproxy")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: hostproxy.Name, Namespace: hostproxy.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		dep, err := r.deploymentForHostproxy(hostproxy)
		if err != nil {
			log.Error(err, "Failed to define new Deployment resource for Hostproxy")

			// The following implementation will update the status
			meta.SetStatusCondition(&hostproxy.Status.Conditions, metav1.Condition{Type: typeAvailableHostproxy,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", hostproxy.Name, err)})

			if err := r.Status().Update(ctx, hostproxy); err != nil {
				log.Error(err, "Failed to update Hostproxy status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, dep); err != nil {
			log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}

		// Deployment created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	foundService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: hostproxy.Name, Namespace: hostproxy.Namespace}, foundService)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new service
		svc, err := r.serviceForHostproxy(hostproxy)
		if err != nil {
			log.Error(err, "Failed to define new Service resource for Hostproxy")

			// The following implementation will update the status
			meta.SetStatusCondition(&hostproxy.Status.Conditions, metav1.Condition{Type: typeAvailableHostproxy,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service for the custom resource (%s): (%s)", hostproxy.Name, err)})

			if err := r.Status().Update(ctx, hostproxy); err != nil {
				log.Error(err, "Failed to update Hostproxy status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Service", "Service.Namespace", svc.Namespace, "Service.Name", svc.Name)
		if err = r.Create(ctx, svc); err != nil {
			log.Error(err, "Failed to create new Service", "Deployment.Namespace", svc.Namespace, "Deployment.Name", svc.Name)
			return ctrl.Result{}, err
		}

		// Service created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Service")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// The CRD API is defining that the Hostproxy type, have a HostproxySpec.Size field
	// to set the quantity of Deployment instances is the desired state on the cluster.
	// Therefore, the following code will ensure the Deployment size is the same as defined
	// via the Size spec of the Custom Resource which we are reconciling.
	size := int32(1)
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		if err = r.Update(ctx, found); err != nil {
			log.Error(err, "Failed to update Deployment",
				"Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)

			// Re-fetch the hostproxy Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, hostproxy); err != nil {
				log.Error(err, "Failed to re-fetch hostproxy")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&hostproxy.Status.Conditions, metav1.Condition{Type: typeAvailableHostproxy,
				Status: metav1.ConditionFalse, Reason: "Resizing",
				Message: fmt.Sprintf("Failed to update the size for the custom resource (%s): (%s)", hostproxy.Name, err)})

			if err := r.Status().Update(ctx, hostproxy); err != nil {
				log.Error(err, "Failed to update Hostproxy status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Now, that we update the size we want to requeue the reconciliation
		// so that we can ensure that we have the latest state of the resource before
		// update. Also, it will help ensure the desired state on the cluster
		return ctrl.Result{Requeue: true}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(
		&hostproxy.Status.Conditions,
		metav1.Condition{
			Type:   typeAvailableHostproxy,
			Status: metav1.ConditionTrue, Reason: "Reconciling",
			Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", hostproxy.Name, size),
		},
	)

	if err := r.Status().Update(ctx, hostproxy); err != nil {
		log.Error(err, "Failed to update Hostproxy status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalizeHostproxy will perform the required operations before delete the CR.
func (r *HostproxyReconciler) doFinalizerOperationsForHostproxy(cr *networkingv1.Hostproxy) {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of delete resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as depended of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			cr.Name,
			cr.Namespace))
}

// deploymentForHostproxy returns a Hostproxy Deployment object
func (r *HostproxyReconciler) deploymentForHostproxy(
	hostproxy *networkingv1.Hostproxy) (*appsv1.Deployment, error) {
	ls := labelsForHostproxy(hostproxy.Name)
	replicas := int32(1)

	// Get the Operand image
	image, err := imageForHostproxy()
	if err != nil {
		return nil, err
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostproxy.Name,
			Namespace: hostproxy.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Image:           image,
						Name:            "hostproxy",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: &corev1.SecurityContext{
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{
									"NET_ADMIN",
									"NET_RAW",
								},
							},
						},
						Env: []corev1.EnvVar{
							{
								Name:  "PORTS",
								Value: fmt.Sprintf("%d:%d", hostproxy.Spec.ClusterPort, hostproxy.Spec.HostPort),
							},
						},
					}},
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(hostproxy, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

func (r *HostproxyReconciler) serviceForHostproxy(hostproxy *networkingv1.Hostproxy) (*corev1.Service, error) {
	ls := labelsForHostproxy(hostproxy.Name)
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostproxy.Name,
			Namespace: hostproxy.Namespace,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  ls,
		},
	}

	// Set the ownerRef for the Service
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(hostproxy, svc, r.Scheme); err != nil {
		return nil, err
	}
	return svc, nil
}

// labelsForHostproxy returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForHostproxy(name string) map[string]string {
	var imageTag string
	image, err := imageForHostproxy()
	if err == nil {
		imageTag = strings.Split(image, ":")[1]
	}
	return map[string]string{"app.kubernetes.io/name": "Hostproxy",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/version":    imageTag,
		"app.kubernetes.io/part-of":    "hostproxy",
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

// imageForHostproxy gets the Operand image which is managed by this controller
// from the HOSTPROXY_IMAGE environment variable defined in the config/manager/manager.yaml
func imageForHostproxy() (string, error) {
	var imageEnvVar = "HOSTPROXY_IMAGE"
	image, found := os.LookupEnv(imageEnvVar)
	if !found {
		return "", fmt.Errorf("Unable to find %s environment variable with the image", imageEnvVar)
	}
	return image, nil
}

// SetupWithManager sets up the controller with the Manager.
// Note that the Deployment will be also watched in order to ensure its
// desirable state on the cluster
func (r *HostproxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Hostproxy{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
