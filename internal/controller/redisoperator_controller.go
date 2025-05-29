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
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
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
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ivangonzalezacunav1alpha1 "github.com/ivangonzalezacuna/redis-operator/api/v1alpha1"
)

// const redisOperatorFinalizer = "ivangonzalezacuna/finalizer"

// Definitions to manage status conditions
const (
	// typeAvailableRedis represents the status of the Deployment reconciliation
	typeAvailableRedis = "Available"
	// typeDegradedRedis represents the status used when the custom resource is deleted and the finalizer operations are yet to occur.
	typeDegradedRedis = "Degraded"
)

// RedisOperatorReconciler reconciles a RedisOperator object
type RedisOperatorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ivangonzalezacuna.docker.io,resources=redisoperators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ivangonzalezacuna.docker.io,resources=redisoperators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ivangonzalezacuna.docker.io,resources=redisoperators/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *RedisOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	redisOperator := &ivangonzalezacunav1alpha1.RedisOperator{}
	err := r.Get(ctx, req.NamespacedName, redisOperator)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("redis-operator resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get redis-operator")
		return ctrl.Result{}, err
	}

	// Let's just set the status as Unknown when no status is available
	if len(redisOperator.Status.Conditions) == 0 {
		meta.SetStatusCondition(&redisOperator.Status.Conditions, metav1.Condition{Type: typeAvailableRedis, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, redisOperator); err != nil {
			log.Error(err, "Failed to update Redis Operator status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the redis-operator Custom Resource after updating the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raising the error "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, redisOperator); err != nil {
			log.Error(err, "Failed to re-fetch redis-operator")
			return ctrl.Result{}, err
		}
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: redisOperator.Name, Namespace: redisOperator.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		sec, err := r.redisSecret(redisOperator)
		if err != nil {
			log.Error(err, "Failed to define new Secret resource for Redis Operator")

			// The following implementation will update the status
			meta.SetStatusCondition(&redisOperator.Status.Conditions, metav1.Condition{Type: typeDegradedRedis,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Secret for the custom resource (%s): (%s)", redisOperator.Name, err)})

			if err := r.Status().Update(ctx, redisOperator); err != nil {
				log.Error(err, "Failed to update Redis Operator status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Define a new deployment
		dep, err := r.redisDeployment(redisOperator)
		if err != nil {
			log.Error(err, "Failed to define new Deployment resource for Redis Operator")

			// The following implementation will update the status
			meta.SetStatusCondition(&redisOperator.Status.Conditions, metav1.Condition{Type: typeDegradedRedis,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", redisOperator.Name, err)})

			if err := r.Status().Update(ctx, redisOperator); err != nil {
				log.Error(err, "Failed to update Redis Operator status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		r.Recorder.Event(sec, "Normal", "Creating", fmt.Sprintf("Custom Resource %s is being created from the namespace %s", sec.Name, sec.Namespace))

		log.Info("Creating a new Secret", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, sec); err != nil {
			log.Error(err, "Failed to create new Secret", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}

		r.Recorder.Event(dep, "Normal", "Creating", fmt.Sprintf("Custom Resource %s is being created from the namespace %s", dep.Name, dep.Namespace))

		log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, dep); err != nil {
			log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}

		// Deployment and Secret created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// The CRD API defines that the RedisOperator type have a RedisOperator.Replicas field
	// to set the quantity of Deployment instances to the desired state on the cluster.
	// Therefore, the following code will ensure the Deployment replicas are the same as defined
	// via the Replicas spec of the Custom Resource which we are reconciling.
	replicas := redisOperator.Spec.Replicas
	if *found.Spec.Replicas != replicas {
		found.Spec.Replicas = &replicas
		if err = r.Update(ctx, found); err != nil {
			log.Error(err, "Failed to update Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)

			// Re-fetch the redis-operator Custom Resource before updating the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raising the error "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, redisOperator); err != nil {
				log.Error(err, "Failed to re-fetch redis-operator")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&redisOperator.Status.Conditions, metav1.Condition{Type: typeDegradedRedis,
				Status: metav1.ConditionFalse, Reason: "Resizing",
				Message: fmt.Sprintf("Failed to update the replicas for the custom resource (%s): (%s)", redisOperator.Name, err)})

			if err := r.Status().Update(ctx, redisOperator); err != nil {
				log.Error(err, "Failed to update Redis Operator status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		r.Recorder.Event(redisOperator, "Normal", "Resizing", fmt.Sprintf("Custom Resource %s is being resized to %d replicas", redisOperator.Name, replicas))

		// Now, that we update the replicas we want to requeue the reconciliation
		// so that we can ensure that we have the latest state of the resource before
		// update. Also, it will help ensure the desired state on the cluster
		return ctrl.Result{Requeue: true}, nil
	}

	// The CRD API defines that the RedisOperator type have a RedisOperator.Port field
	// to set the port where Redis is listening.
	// Therefore, the following code will ensure the Deployment port configuration is
	// the same as defined via the Port spec of the Custom Resource which we are reconciling.
	// To apply the updates, we are making the assumption that the deployment is still
	// matching the same configuration we defined when creating it.
	redisPort := redisOperator.Spec.Port
	if len(found.Spec.Template.Spec.Containers) == 1 &&
		len(found.Spec.Template.Spec.Containers[0].Env) == 2 &&
		found.Spec.Template.Spec.Containers[0].Env[0].Name == "REDIS_PORT_NUMBER" &&
		len(found.Spec.Template.Spec.Containers[0].Ports) == 1 &&
		found.Spec.Template.Spec.Containers[0].Ports[0].Name == "redis" &&
		found.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort != redisPort {
		found.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort = redisPort
		found.Spec.Template.Spec.Containers[0].Env[0].Value = strconv.Itoa(int(redisPort))

		if err = r.Update(ctx, found); err != nil {
			log.Error(err, "Failed to update Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)

			// Re-fetch the redis-operator Custom Resource before updating the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raising the error "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, redisOperator); err != nil {
				log.Error(err, "Failed to re-fetch redis-operator")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&redisOperator.Status.Conditions, metav1.Condition{Type: typeDegradedRedis,
				Status: metav1.ConditionFalse, Reason: "Restarting",
				Message: fmt.Sprintf("Failed to update the port for the custom resource (%s): (%s)", redisOperator.Name, err)})

			if err := r.Status().Update(ctx, redisOperator); err != nil {
				log.Error(err, "Failed to update Redis Operator status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Now, that we update the redis port we want to requeue the reconciliation
		// so that we can ensure that we have the latest state of the resource before
		// update. Also, it will help ensure the desired state on the cluster
		return ctrl.Result{Requeue: true}, nil

	}

	// The following implementation will update the status
	meta.SetStatusCondition(&redisOperator.Status.Conditions, metav1.Condition{Type: typeAvailableRedis,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", redisOperator.Name, replicas)})

	if err := r.Status().Update(ctx, redisOperator); err != nil {
		log.Error(err, "Failed to update Redis Operator status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RedisOperatorReconciler) redisSecret(redisOperator *ivangonzalezacunav1alpha1.RedisOperator) (*corev1.Secret, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	// Metadata for creating secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "redis-secret",
			Namespace:         redisOperator.Namespace,
			CreationTimestamp: metav1.Now(),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(token),
		},
	}

	err = ctrl.SetControllerReference(redisOperator, secret, r.Scheme)
	if err != nil {
		return nil, err
	}

	return secret, nil
}

// Generate the Random 256 byte token
func generateToken() (string, error) {
	token := make([]byte, 256)
	_, err := rand.Read(token)
	if err != nil {
		return "", fmt.Errorf("found error while generating the secret token: %w", err)
	}

	return base64.StdEncoding.EncodeToString(token), nil
}

func (r *RedisOperatorReconciler) redisDeployment(redisOperator *ivangonzalezacunav1alpha1.RedisOperator) (*appsv1.Deployment, error) {
	redisImage := "bitnami/redis:8.0.2"
	ls := map[string]string{
		"app.kubernetes.io/name":       "redis-operator",
		"app.kubernetes.io/managed-by": "RedisOperator",
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              redisOperator.Name,
			Namespace:         redisOperator.Namespace,
			CreationTimestamp: metav1.Now(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &redisOperator.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
					Labels:            ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "redis",
							Image:           redisImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: redisOperator.Spec.Port,
									Name:          "redis",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "REDIS_PORT_NUMBER",
									Value: strconv.Itoa(int(redisOperator.Spec.Port)),
								},
								{
									Name: "REDIS_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "redis-secret",
											},
											Key: "password",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err := ctrl.SetControllerReference(redisOperator, deployment, r.Scheme)
	if err != nil {
		return nil, err
	}

	return deployment, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RedisOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ivangonzalezacunav1alpha1.RedisOperator{}).
		Named("redisoperator").
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
