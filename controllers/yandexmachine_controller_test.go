/*
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

package controllers //nolint:testpackage // private variables access

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/client/mock_client"
	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/scope"
	loadbalancer "github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/services/loadbalancers"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "github.com/yandex-cloud/cluster-api-provider-yandex/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("YandexMachine reconciliation check", func() {
	BeforeEach(func() {
		var err error

		e = ClusterTestEnv{
			Client:           k8sClient,
			clusterName:      "machine-check-cluster",
			machineName:      "test",
			secretName:       "test",
			reconcileTimeout: 3 * time.Second,
		}
		//+kubebuilder:scaffold:webhook
		e.controller = gomock.NewController(GinkgoT())
		e.mockClient = mock_client.NewMockClient(e.controller)
		e.mockClientGetter = mock_client.NewMockYandexClientGetter(e.controller)
		e.mockClientGetter.EXPECT().GetDefault(gomock.Any()).
			DoAndReturn(func(_ context.Context) (*mock_client.MockClient, error) {
				return e.mockClient, nil
			}).AnyTimes()
		e.mockClient.EXPECT().Close(gomock.Any()).Return(nil).AnyTimes()
		testNamespace, err = e.CreateNamespace(ctx, "machine-check")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(e.DeleteNamespace(ctx)).To(Succeed())
		e.controller.Finish()
	})

	When("Creating an empty YandexMachine", func() {
		It("should not fail", func() {
			Expect(e.Create(ctx, e.getYandexMachine(testNamespace.Name))).To(Succeed())
		})
	})

	When("Creating an YandexMachine", func() {
		It("should not reconcile YandexMachine without parent CAPI Machine", func() {
			ym := e.getYandexMachine(testNamespace.Name)
			Expect(e.Create(ctx, ym)).To(Succeed())

			reconciler := &YandexMachineReconciler{
				Client:             k8sClient,
				YandexClientGetter: e.mockClientGetter,
			}

			result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("should not reconcile YandexMachine without parent YandexCluster", func() {
			Expect(e.Create(ctx, e.getMachineWithInfrastructureRef(testNamespace.Name))).To(Succeed())
			ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
			Expect(e.Create(ctx, ym)).To(Succeed())

			reconciler := &YandexMachineReconciler{
				Client:             k8sClient,
				YandexClientGetter: e.mockClientGetter,
			}

			result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("should not reconcile YandexMachine with paused CAPI Cluster", func() {
			cc := e.getCAPIClusterWithInfrastructureReference(testNamespace.Name)
			cc.Spec.Paused = ptr.To(true)
			Expect(e.Create(ctx, cc)).To(Succeed())
			Expect(e.Create(ctx, e.getYandexClusterWithOwnerReference(testNamespace.Name))).To(Succeed())
			Expect(e.Create(ctx, e.getMachineWithInfrastructureRef(testNamespace.Name))).To(Succeed())
			ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
			Expect(e.Create(ctx, ym)).To(Succeed())

			reconciler := &YandexMachineReconciler{
				Client:             k8sClient,
				YandexClientGetter: e.mockClientGetter,
			}
			result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			// v1beta2 contract: the paused state is surfaced in the Paused condition,
			// and a paused YandexMachine is not touched otherwise (no finalizer, no cloud calls).
			ym = &infrav1.YandexMachine{}
			Eventually(func() bool {
				key := client.ObjectKey{Name: e.machineName, Namespace: testNamespace.Name}
				err := e.Get(ctx, key, ym)
				return err == nil && conditions.IsTrue(ym, infrav1.PausedCondition)
			}, e.reconcileTimeout).Should(BeTrue())
			Expect(ym.GetFinalizers()).To(BeEmpty())
			Expect(ym.Spec.ProviderID).To(BeEmpty())
			expectMachineNotProvisioned(ym)
		})
	})

	It("should not error and get ready status eventually", func() {
		Expect(e.Create(ctx, e.getCAPIClusterWithInfrastructureReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getYandexClusterWithOwnerReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getMachineWithInfrastructureRef(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getBootstrapSecret(testNamespace.Name))).To(Succeed())
		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		Expect(e.Create(ctx, ym)).To(Succeed())

		reconciler := &YandexMachineReconciler{
			Client:             k8sClient,
			YandexClientGetter: e.mockClientGetter,
		}

		addr := "1.2.3.4"
		e.setNewYandexMachineReconcileMocks(addr)
		result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).NotTo(HaveOccurred())

		// On the first reconcile the YandexMachine have to be in STARTING status.
		ym = &infrav1.YandexMachine{}
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}
			err = e.Get(ctx, key, ym)
			return (err == nil &&
				ym.Status.InstanceStatus != nil &&
				*ym.Status.InstanceStatus == infrav1.InstanceStatusStarting)
		}, e.reconcileTimeout).Should(BeTrue())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		expectMachineNotProvisioned(ym)
		Expect(ym.GetFinalizers()).To(Equal([]string{infrav1.MachineFinalizer}))

		result, err = reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).NotTo(HaveOccurred())

		// On the second reconcile the YandexMachine have to be READY.
		ym = &infrav1.YandexMachine{}
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}
			err := e.Get(ctx, key, ym)
			return (err == nil && machineReady(ym))
		}, e.reconcileTimeout).Should(BeTrue())
		Expect(result.RequeueAfter).To(BeZero())
		Expect(ym.Status.Addresses[0].Address).To(Equal(addr))

		// v1beta2 contract fields.
		Expect(ym.Status.Initialization.Provisioned).To(HaveValue(BeTrue()))
		Expect(ym.Spec.ProviderID).To(Equal("yandex://123"))
		Expect(conditions.IsFalse(ym, infrav1.PausedCondition)).To(BeTrue())
		Expect(conditions.GetReason(ym, infrav1.InstanceReadyCondition)).To(Equal(infrav1.InstanceRunningReason))
		Expect(conditions.GetReason(ym, infrav1.ReadyCondition)).To(Equal(infrav1.ReadyReason))
	})

	It("should neither restart nor recreate a stopped instance and become ready again when it runs", func() {
		Expect(e.Create(ctx, e.getCAPIClusterWithInfrastructureReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getYandexClusterWithOwnerReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getMachineWithInfrastructureRef(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getBootstrapSecret(testNamespace.Name))).To(Succeed())
		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		Expect(e.Create(ctx, ym)).To(Succeed())

		reconciler := &YandexMachineReconciler{
			Client:             k8sClient,
			YandexClientGetter: e.mockClientGetter,
		}

		// The mock is strict: exactly one ComputeCreate and no ComputeDelete are allowed, so any
		// attempt to recreate or delete the stopped instance fails the test.
		addr := "1.2.3.4"
		e.setStoppedAndStartedYandexMachineReconcileMocks(addr)
		get := func() *infrav1.YandexMachine {
			GinkgoHelper()
			obj := &infrav1.YandexMachine{}
			Expect(e.Get(ctx, client.ObjectKey{Name: e.machineName, Namespace: testNamespace.Name}, obj)).To(Succeed())
			return obj
		}

		// 1. The instance is created and running: the machine is provisioned and ready.
		result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
		Eventually(func() bool { return machineReady(get()) }, e.reconcileTimeout).Should(BeTrue())
		providerID := get().Spec.ProviderID
		Expect(providerID).ToNot(BeEmpty())

		// 2. The instance is stopped in the cloud: the state is only reported.
		result, err = reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		Eventually(func() bool {
			return conditions.IsFalse(get(), infrav1.InstanceReadyCondition)
		}, e.reconcileTimeout).Should(BeTrue())
		ym = get()
		Expect(*ym.Status.InstanceStatus).To(Equal(infrav1.InstanceStatusStopped))
		Expect(conditions.GetReason(ym, infrav1.InstanceReadyCondition)).To(Equal(infrav1.InstanceNotRunningReason))
		Expect(conditions.IsFalse(ym, infrav1.ReadyCondition)).To(BeTrue())
		// Initial provisioning has been completed and stays completed, the instance is the same.
		Expect(ym.Status.Initialization.Provisioned).To(HaveValue(BeTrue()))
		Expect(ym.Spec.ProviderID).To(Equal(providerID))

		// 3. The instance is started again: the machine becomes ready without being recreated.
		result, err = reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
		Eventually(func() bool { return machineReady(get()) }, e.reconcileTimeout).Should(BeTrue())
		Expect(get().Spec.ProviderID).To(Equal(providerID))
	})

	It("should not error and add controlplane node to application load balancer in cluster with ALB", func() {
		Expect(e.Create(ctx, e.getCAPIClusterWithInfrastructureReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getYandexClusterWithOwnerReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getCPMachineWithInfrastructureRef(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getBootstrapSecret(testNamespace.Name))).To(Succeed())
		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		Expect(e.Create(ctx, ym)).To(Succeed())

		addr := "1.2.3.4"
		tgName := "targetgroup"
		e.setNewCPYandexMachineReconcileMocks(addr, tgName)
		reconciler := &YandexMachineReconciler{
			Client:             k8sClient,
			YandexClientGetter: e.mockClientGetter,
		}

		result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).NotTo(HaveOccurred())

		ym = &infrav1.YandexMachine{}
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}
			err := e.Get(ctx, key, ym)
			return (err == nil && machineReady(ym))
		}, e.reconcileTimeout).Should(BeTrue())

		Expect(result.RequeueAfter).To(BeZero())
		Expect(ym.Status.Addresses[0].Address).To(Equal(addr))
	})

	It("should error and start machine creation again on api error", func() {
		Expect(e.Create(ctx, e.getCAPIClusterWithInfrastructureReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getYandexClusterWithOwnerReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getMachineWithInfrastructureRef(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getBootstrapSecret(testNamespace.Name))).To(Succeed())
		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		Expect(e.Create(ctx, ym)).To(Succeed())

		reconciler := &YandexMachineReconciler{
			Client:             k8sClient,
			YandexClientGetter: e.mockClientGetter,
		}

		e.setNewYandexMachineErrorReconcileMocks()
		// got machine creation api error.
		result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).To(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
		ym = &infrav1.YandexMachine{}
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}
			err = e.Get(ctx, key, ym)
			return (err == nil &&
				conditions.IsFalse(ym, infrav1.InstanceReadyCondition) &&
				conditions.GetReason(ym, infrav1.InstanceReadyCondition) == infrav1.InstanceCreateFailedReason)
		}, e.reconcileTimeout).Should(BeTrue())
		Expect(ym.Spec.ProviderID).To(BeEmpty())
		expectMachineNotProvisioned(ym)

		// Machine creation API call ok,  YandexMachine have to be in STARTING status.
		result, err = reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		ym = &infrav1.YandexMachine{}
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}
			err = e.Get(ctx, key, ym)
			return (err == nil &&
				ym.Status.InstanceStatus != nil &&
				*ym.Status.InstanceStatus == infrav1.InstanceStatusStarting)
		}, e.reconcileTimeout).Should(BeTrue())
		expectMachineNotProvisioned(ym)
		Expect(ym.GetFinalizers()).To(Equal([]string{infrav1.MachineFinalizer}))
	})

	It("should fail machine creation when yandex cloud target group was deleted by someone", func() {
		Expect(e.Create(ctx, e.getCAPIClusterWithInfrastructureReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getYandexClusterWithOwnerReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getCPMachineWithInfrastructureRef(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getBootstrapSecret(testNamespace.Name))).To(Succeed())
		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		Expect(e.Create(ctx, ym)).To(Succeed())

		addr := "1.2.3.4"
		e.setNewCPYandexMachineWithoutTargetGroupErrorReconcileMocks(addr)
		reconciler := &YandexMachineReconciler{
			Client:             k8sClient,
			YandexClientGetter: e.mockClientGetter,
		}

		result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).To(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
		ym = &infrav1.YandexMachine{}
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}

			err = e.Get(ctx, key, ym)

			return (err == nil &&
				conditions.IsFalse(ym, infrav1.InstanceReadyCondition) &&
				conditions.GetReason(ym, infrav1.InstanceReadyCondition) == infrav1.InstanceReconcileFailedReason)
		}, e.reconcileTimeout).Should(BeTrue())
		expectMachineNotProvisioned(ym)
	})

	It("should fail machine creation when yandex cloud instance was deleted by someone else", func() {
		Expect(e.Create(ctx, e.getCAPIClusterWithInfrastructureReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getYandexClusterWithOwnerReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getMachineWithInfrastructureRef(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getBootstrapSecret(testNamespace.Name))).To(Succeed())
		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		Expect(e.Create(ctx, ym)).To(Succeed())

		reconciler := &YandexMachineReconciler{
			Client:             k8sClient,
			YandexClientGetter: e.mockClientGetter,
		}

		e.setYandexMachineNotFoundReconcileMocks()
		// got machine creation api error.
		result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		ym = &infrav1.YandexMachine{}
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}
			err = e.Get(ctx, key, ym)
			return (err == nil &&
				ym.Status.InstanceStatus != nil &&
				*ym.Status.InstanceStatus == infrav1.InstanceStatusDeleted)
		}, e.reconcileTimeout).Should(BeTrue())
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}
			err = e.Get(ctx, key, ym)
			return err == nil && conditions.IsFalse(ym, infrav1.ReadyCondition)
		}, e.reconcileTimeout).Should(BeTrue())
		Expect(conditions.IsFalse(ym, infrav1.InstanceReadyCondition)).To(BeTrue())
		Expect(conditions.GetReason(ym, infrav1.InstanceReadyCondition)).To(Equal(infrav1.InstanceNotFoundReason))
		Expect(conditions.GetMessage(ym, infrav1.InstanceReadyCondition)).ToNot(BeEmpty())
		expectMachineNotProvisioned(ym)
	})

	It("should error and retry to add node to ALB target group on load balancer api error", func() {
		Expect(e.Create(ctx, e.getCAPIClusterWithInfrastructureReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getYandexClusterWithOwnerReference(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getCPMachineWithInfrastructureRef(testNamespace.Name))).To(Succeed())
		Expect(e.Create(ctx, e.getBootstrapSecret(testNamespace.Name))).To(Succeed())
		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		Expect(e.Create(ctx, ym)).To(Succeed())

		addr := "1.2.3.4"
		tgName := "targetgroup"
		e.setNewCPYandexMachineErrorReconcileMocks(addr, tgName)
		reconciler := &YandexMachineReconciler{
			Client:             k8sClient,
			YandexClientGetter: e.mockClientGetter,
		}

		// target group add error.
		result, err := reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).To(HaveOccurred())

		ym = &infrav1.YandexMachine{}
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}
			err = e.Get(ctx, key, ym)
			return (err == nil && !machineReady(ym))
		}, e.reconcileTimeout).Should(BeTrue())

		Expect(result.RequeueAfter).To(BeZero())
		Expect(ym.Status.Addresses[0].Address).To(Equal(addr))

		// target group add succeed.
		result, err = reconciler.Reconcile(ctx, e.getReconcileRequest(ym.Namespace, ym.Name))
		Expect(err).NotTo(HaveOccurred())

		ym = &infrav1.YandexMachine{}
		Eventually(func() bool {
			key := client.ObjectKey{
				Name:      e.machineName,
				Namespace: testNamespace.Name,
			}
			err := e.Get(ctx, key, ym)
			return (err == nil && machineReady(ym))
		}, e.reconcileTimeout).Should(BeTrue())

		Expect(result.RequeueAfter).To(BeZero())
		Expect(ym.Status.Addresses[0].Address).To(Equal(addr))
	})
})

var _ = Describe("YandexMachine deletions checks", func() {
	BeforeEach(func() {
		var err error

		e = ClusterTestEnv{
			Client:           k8sClient,
			clusterName:      "machine-delete-cluster",
			machineName:      "test",
			secretName:       "test",
			reconcileTimeout: 3 * time.Second,
		}
		//+kubebuilder:scaffold:webhook
		e.controller = gomock.NewController(GinkgoT())
		e.mockClient = mock_client.NewMockClient(e.controller)
		e.mockClientGetter = mock_client.NewMockYandexClientGetter(e.controller)
		e.mockClientGetter.EXPECT().GetDefault(gomock.Any()).
			DoAndReturn(func(_ context.Context) (*mock_client.MockClient, error) {
				return e.mockClient, nil
			}).AnyTimes()
		e.mockClient.EXPECT().Close(gomock.Any()).Return(nil).AnyTimes()
		testNamespace, err = e.CreateNamespace(ctx, "machine-delete")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(e.DeleteNamespace(ctx)).To(Succeed())
		e.controller.Finish()
	})

	When("Deleting an ordinary YandexMachine", func() {
		It("should delete YandexMachine if ProviderID does not set", func() {
			ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
			controllerutil.AddFinalizer(ym, infrav1.MachineFinalizer)

			reconciler := &YandexMachineReconciler{
				Client:             e.Client,
				YandexClientGetter: e.mockClientGetter,
			}

			clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
				Client:             e.Client,
				Cluster:            e.getCAPIClusterWithInfrastructureReference(testNamespace.Name),
				YandexCluster:      e.getYandexClusterWithOwnerReference(testNamespace.Name),
				YandexClientGetter: e.mockClientGetter,
			})
			Expect(err).NotTo(HaveOccurred())

			machineScope, err := scope.NewMachineScope(scope.MachineScopeParams{
				Client:        e.Client,
				Machine:       e.getMachineWithInfrastructureRef(testNamespace.Name),
				LoadBalancer:  loadbalancer.New(clusterScope),
				ClusterGetter: clusterScope,
				YandexMachine: ym,
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := reconciler.reconcileDelete(ctx, machineScope)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
		})
	})

	It("should delete YandexMachine if providerID is set, but YandexCloud VM does not exists", func() {
		const id string = "123"
		notFoundError := status.Error(codes.NotFound, "instance not found")

		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		controllerutil.AddFinalizer(ym, infrav1.MachineFinalizer)

		reconciler := &YandexMachineReconciler{
			Client:             e.Client,
			YandexClientGetter: e.mockClientGetter,
		}

		clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
			Client:             e.Client,
			Cluster:            e.getCAPIClusterWithInfrastructureReference(testNamespace.Name),
			YandexCluster:      e.getYandexClusterWithOwnerReference(testNamespace.Name),
			YandexClientGetter: e.mockClientGetter,
		})
		Expect(err).NotTo(HaveOccurred())

		machineScope, err := scope.NewMachineScope(scope.MachineScopeParams{
			Client:        e.Client,
			Machine:       e.getMachineWithInfrastructureRef(testNamespace.Name),
			LoadBalancer:  loadbalancer.New(clusterScope),
			ClusterGetter: clusterScope,
			YandexMachine: ym,
		})
		Expect(err).NotTo(HaveOccurred())
		machineScope.SetProviderID(id)

		// There is no YandexCloud VM with this id.
		e.mockClient.EXPECT().ComputeGet(gomock.Any(), id).Return(nil, notFoundError)
		result, err := reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("should delete YandexMachine if providerID is set and YandexCloud VM does exists", func() {
		const id string = "123"

		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		controllerutil.AddFinalizer(ym, infrav1.MachineFinalizer)

		reconciler := &YandexMachineReconciler{
			Client:             e.Client,
			YandexClientGetter: e.mockClientGetter,
		}

		clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
			Client:             e.Client,
			Cluster:            e.getCAPIClusterWithInfrastructureReference(testNamespace.Name),
			YandexCluster:      e.getYandexClusterWithOwnerReference(testNamespace.Name),
			YandexClientGetter: e.mockClientGetter,
		})
		Expect(err).NotTo(HaveOccurred())

		machineScope, err := scope.NewMachineScope(scope.MachineScopeParams{
			Client:        e.Client,
			Machine:       e.getMachineWithInfrastructureRef(testNamespace.Name),
			LoadBalancer:  loadbalancer.New(clusterScope),
			ClusterGetter: clusterScope,
			YandexMachine: ym,
		})
		Expect(err).NotTo(HaveOccurred())
		machineScope.SetProviderID(id)

		// Send compute deletion request.
		e.setYandexMachineDeleteMocks(id)
		result, err := reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		status := machineScope.GetInstanceStatus()
		Expect(status).NotTo(BeNil())
		Expect(*status).To(Equal(infrav1.InstanceStatusRunning))

		// Get DELETING status.
		result, err = reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		status = machineScope.GetInstanceStatus()
		Expect(status).NotTo(BeNil())
		Expect(*status).To(Equal(infrav1.InstanceStatusDeleting))

		// YandexCloud VM deleted.
		result, err = reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(controllerutil.ContainsFinalizer(machineScope.YandexMachine, infrav1.ClusterFinalizer)).To(BeFalse())
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("should not delete YandexMachine until receive NOT_FOUND return code from YC compute API", func() {
		const id string = "123"

		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		controllerutil.AddFinalizer(ym, infrav1.MachineFinalizer)

		reconciler := &YandexMachineReconciler{
			Client:             e.Client,
			YandexClientGetter: e.mockClientGetter,
		}

		clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
			Client:             e.Client,
			Cluster:            e.getCAPIClusterWithInfrastructureReference(testNamespace.Name),
			YandexCluster:      e.getYandexClusterWithOwnerReference(testNamespace.Name),
			YandexClientGetter: e.mockClientGetter,
		})
		Expect(err).NotTo(HaveOccurred())

		machineScope, err := scope.NewMachineScope(scope.MachineScopeParams{
			Client:        e.Client,
			Machine:       e.getMachineWithInfrastructureRef(testNamespace.Name),
			LoadBalancer:  loadbalancer.New(clusterScope),
			ClusterGetter: clusterScope,
			YandexMachine: ym,
		})
		Expect(err).NotTo(HaveOccurred())
		machineScope.SetProviderID(id)

		// Send compute deletion request.
		e.setYandexMachineDeleteErrorMocks(id)
		result, err := reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		status := machineScope.GetInstanceStatus()
		Expect(status).NotTo(BeNil())
		Expect(*status).To(Equal(infrav1.InstanceStatusRunning))

		// Get DELETING status.
		result, err = reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		status = machineScope.GetInstanceStatus()
		Expect(status).NotTo(BeNil())
		Expect(*status).To(Equal(infrav1.InstanceStatusDeleting))

		// When we are get error from YC API on instance request and error status
		// does not equal to NOT_FOUND error, we are should not delete the YandexMachine.
		// We should return with API error and restart reconciliation flow.
		result, err = reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).To(HaveOccurred())
		Expect(ym.GetFinalizers()).To(Equal([]string{infrav1.MachineFinalizer}))
		status = machineScope.GetInstanceStatus()
		Expect(status).NotTo(BeNil())
		Expect(*status).To(Equal(infrav1.InstanceStatusDeleting))
	})

	It("should delete control plane YandexMachine and remove it from load balancer if YandexCloud VM does exists", func() {
		const (
			id      string = "123"
			address string = "1.2.3.4"
		)

		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		controllerutil.AddFinalizer(ym, infrav1.MachineFinalizer)
		ym.Status.Addresses = append(ym.Status.Addresses, corev1.NodeAddress{
			Address: address,
		})

		reconciler := &YandexMachineReconciler{
			Client:             e.Client,
			YandexClientGetter: e.mockClientGetter,
		}

		clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
			Client:             e.Client,
			Cluster:            e.getCAPIClusterWithInfrastructureReference(testNamespace.Name),
			YandexCluster:      e.getYandexClusterWithOwnerReference(testNamespace.Name),
			YandexClientGetter: e.mockClientGetter,
		})
		Expect(err).NotTo(HaveOccurred())

		// No API defaults here, so we have to set load balancer type
		clusterScope.YandexCluster.Spec.LoadBalancer.Type = infrav1.LoadBalancerTypeALB
		machineScope, err := scope.NewMachineScope(scope.MachineScopeParams{
			Client:        e.Client,
			Machine:       e.getCPMachineWithInfrastructureRef(testNamespace.Name),
			LoadBalancer:  loadbalancer.New(clusterScope),
			ClusterGetter: clusterScope,
			YandexMachine: ym,
		})
		Expect(err).NotTo(HaveOccurred())
		machineScope.SetProviderID(id)

		// Deregister from ALB and send compute deletion request.
		e.setCPYandexMachineDeleteMocks(id, address)
		result, err := reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		status := machineScope.GetInstanceStatus()
		Expect(status).NotTo(BeNil())
		Expect(*status).To(Equal(infrav1.InstanceStatusRunning))

		// Get DELETING status.
		result, err = reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		status = machineScope.GetInstanceStatus()
		Expect(status).NotTo(BeNil())
		Expect(*status).To(Equal(infrav1.InstanceStatusDeleting))

		// YandexCloud VM deleted.
		result, err = reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(controllerutil.ContainsFinalizer(machineScope.YandexMachine, infrav1.ClusterFinalizer)).To(BeFalse())
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("should delete control plane YandexMachine without target group", func() {
		const (
			id      string = "123"
			address string = "1.2.3.4"
		)

		ym := e.getYandexMachineWithOwnerRef(testNamespace.Name)
		controllerutil.AddFinalizer(ym, infrav1.MachineFinalizer)
		ym.Status.Addresses = append(ym.Status.Addresses, corev1.NodeAddress{
			Address: address,
		})

		reconciler := &YandexMachineReconciler{
			Client:             e.Client,
			YandexClientGetter: e.mockClientGetter,
		}

		clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
			Client:             e.Client,
			Cluster:            e.getCAPIClusterWithInfrastructureReference(testNamespace.Name),
			YandexCluster:      e.getYandexClusterWithOwnerReference(testNamespace.Name),
			YandexClientGetter: e.mockClientGetter,
		})
		Expect(err).NotTo(HaveOccurred())

		// No API defaults here, so we have to set load balancer type
		clusterScope.YandexCluster.Spec.LoadBalancer.Type = infrav1.LoadBalancerTypeALB
		machineScope, err := scope.NewMachineScope(scope.MachineScopeParams{
			Client:        e.Client,
			Machine:       e.getCPMachineWithInfrastructureRef(testNamespace.Name),
			LoadBalancer:  loadbalancer.New(clusterScope),
			ClusterGetter: clusterScope,
			YandexMachine: ym,
		})
		Expect(err).NotTo(HaveOccurred())
		machineScope.SetProviderID(id)

		// Deregister from ALB and send compute deletion request.
		e.setCPYandexMachineWithoutTargetGroupDeleteMocks(id, address)
		result, err := reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		status := machineScope.GetInstanceStatus()
		Expect(status).NotTo(BeNil())
		Expect(*status).To(Equal(infrav1.InstanceStatusRunning))

		// Get DELETING status.
		result, err = reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDuration))
		status = machineScope.GetInstanceStatus()
		Expect(status).NotTo(BeNil())
		Expect(*status).To(Equal(infrav1.InstanceStatusDeleting))

		// YandexCloud VM deleted.
		result, err = reconciler.reconcileDelete(ctx, machineScope)
		Expect(err).NotTo(HaveOccurred())
		Expect(controllerutil.ContainsFinalizer(machineScope.YandexMachine, infrav1.ClusterFinalizer)).To(BeFalse())
		Expect(result.RequeueAfter).To(BeZero())
	})
})
