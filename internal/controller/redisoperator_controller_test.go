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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ivangonzalezacunav1alpha1 "github.com/ivangonzalezacuna/redis-operator/api/v1alpha1"
)

var _ = Describe("RedisOperator Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const resourceReplicas = 3

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "test-namespace",
		}
		redisoperator := &ivangonzalezacunav1alpha1.RedisOperator{}

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			err := k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      typeNamespacedName.Namespace,
					Namespace: typeNamespacedName.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Creating the custom resource for the Kind RedisOperator")
			err = k8sClient.Get(ctx, typeNamespacedName, redisoperator)
			if err != nil && errors.IsNotFound(err) {
				resource := &ivangonzalezacunav1alpha1.RedisOperator{
					ObjectMeta: metav1.ObjectMeta{
						Name:      typeNamespacedName.Name,
						Namespace: typeNamespacedName.Namespace,
					},
					Spec: ivangonzalezacunav1alpha1.RedisOperatorSpec{
						Replicas: resourceReplicas,
						Port:     6464,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &ivangonzalezacunav1alpha1.RedisOperator{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance RedisOperator")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Checking if the custom resource was successfully created")
			Eventually(func() error {
				found := &ivangonzalezacunav1alpha1.RedisOperator{}
				return k8sClient.Get(ctx, typeNamespacedName, found)
			}, 10*time.Second, time.Second).Should(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &RedisOperatorReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &record.FakeRecorder{},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if Resources were successfully created in the reconciliation")
			Eventually(func() error {
				var deploymentList appsv1.DeploymentList
				var secretsList corev1.SecretList

				opts := []client.ListOption{
					client.InNamespace(typeNamespacedName.Namespace),
				}

				err = k8sClient.List(context.Background(), &deploymentList, opts...)
				if len(deploymentList.Items) != 1 {
					return fmt.Errorf("Expected 1 deployment, but found %d", len(deploymentList.Items))
				}
				err = k8sClient.List(context.Background(), &secretsList, opts...)
				if len(secretsList.Items) != 1 {
					return fmt.Errorf("Expected 1 secret, but found %d", len(secretsList.Items))
				}

				return nil
			}, 10*time.Second, time.Second).Should(Succeed())
		})
	})
})
