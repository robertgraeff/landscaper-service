// SPDX-FileCopyrightText: 2021 "SAP SE or an SAP affiliate company and Gardener contributors"
//
// SPDX-License-Identifier: Apache-2.0

package instances_test

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	lsserrors "github.com/gardener/landscaper-service/pkg/apis/errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	lsv1alpha1 "github.com/gardener/landscaper/apis/core/v1alpha1"
	kutil "github.com/gardener/landscaper/controller-utils/pkg/kubernetes"
	"github.com/gardener/landscaper/controller-utils/pkg/logging"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	lssv1alpha1 "github.com/gardener/landscaper-service/pkg/apis/core/v1alpha1"
	lsinstallation "github.com/gardener/landscaper-service/pkg/apis/installation"
	instancescontroller "github.com/gardener/landscaper-service/pkg/controllers/instances"
	"github.com/gardener/landscaper-service/pkg/operation"
	"github.com/gardener/landscaper-service/pkg/utils"
	testutils "github.com/gardener/landscaper-service/test/utils"
	"github.com/gardener/landscaper-service/test/utils/envtest"
)

var _ = Describe("Reconcile", func() {
	var (
		op    *operation.Operation
		ctrl  *instancescontroller.Controller
		ctx   context.Context
		state *envtest.State
	)

	BeforeEach(func() {
		ctx = context.Background()
		op = operation.NewOperation(testenv.Client, envtest.LandscaperServiceScheme, testutils.DefaultControllerConfiguration())
		ctrl = instancescontroller.NewTestActuator(*op, logging.Discard())
	})

	AfterEach(func() {
		defer ctx.Done()
		if state != nil {
			Expect(testenv.CleanupResources(ctx, state)).ToNot(HaveOccurred())
		}
	})

	It("should set finalizer and update observed generation", func() {
		var err error
		state, err = testenv.InitResources(ctx, "./testdata/reconcile/test1")
		Expect(err).ToNot(HaveOccurred())

		instance := state.GetInstance("test")

		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())
		Expect(kutil.HasFinalizer(instance, lssv1alpha1.LandscaperServiceFinalizer)).To(BeTrue())
		Expect(instance.Status.ObservedGeneration).To(Equal(int64(1)))
	})

	It("should create a context, target and an installation and handle the data exports", func() {
		var err error
		state, err = testenv.InitResources(ctx, "./testdata/reconcile/test2")
		Expect(err).ToNot(HaveOccurred())

		instance := state.GetInstance("test")
		config := state.GetConfig("default")

		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())
		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())

		Expect(instance.Status.TargetRef).ToNot(BeNil())
		Expect(instance.Status.InstallationRef).ToNot(BeNil())

		context := &lsv1alpha1.Context{}
		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: instance.Status.ContextRef.Name, Namespace: instance.Status.ContextRef.Namespace}, context)).To(Succeed())
		Expect(context.RepositoryContext).ToNot(BeNil())
		Expect(context.RepositoryContext.Type).To(Equal("ociRegistry"))

		target := &lsv1alpha1.Target{}
		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: instance.Status.TargetRef.Name, Namespace: instance.Status.TargetRef.Namespace}, target)).To(Succeed())

		installation := &lsv1alpha1.Installation{}
		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: instance.Status.InstallationRef.Name, Namespace: instance.Status.InstallationRef.Namespace}, installation)).To(Succeed())
		Expect(installation.Spec.Context).To(ContainSubstring("test-"))
		Expect(installation.Spec.ComponentDescriptor.Reference.Version).To(Equal(op.Config().LandscaperServiceComponent.Version))
		Expect(installation.Spec.ComponentDescriptor.Reference.ComponentName).To(Equal(op.Config().LandscaperServiceComponent.Name))
		Expect(installation.Spec.ImportDataMappings[lsinstallation.ProviderTypeImportName]).To(Equal(utils.StringToAnyJSON(config.Spec.ProviderType)))
		Expect(installation.Spec.ImportDataMappings[lsinstallation.VirtualClusterNamespaceImportName]).To(Equal(utils.StringToAnyJSON(lsinstallation.VirtualClusterNamespace)))
		Expect(installation.Spec.ImportDataMappings[lsinstallation.HostingClusterNamespaceImportName]).To(Equal(utils.StringToAnyJSON("12345-abcdef")))

		Expect(installation.Annotations).ToNot(BeNil())
		Expect(installation.Annotations).To(HaveKey(lsv1alpha1.OperationAnnotation))
		Expect(installation.Annotations[lsv1alpha1.OperationAnnotation]).To(Equal(string(lsv1alpha1.ReconcileOperation)))

		landscaperConfigRaw := installation.Spec.ImportDataMappings[lsinstallation.LandscaperConfigImportName]
		Expect(landscaperConfigRaw).ToNot(BeNil())
		landscaperConfig := &lsinstallation.LandscaperConfig{}
		Expect(json.Unmarshal(landscaperConfigRaw.RawMessage, landscaperConfig)).To(Succeed())
		Expect(landscaperConfig.Deployers).To(ContainElements("helm", "container", "manifest"))

		clusterEndpoint := "10.0.0.1:1234"
		endpointExport := &lsv1alpha1.DataObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "endpointexport",
				Namespace: state.Namespace,
				Labels: map[string]string{
					lsv1alpha1.DataObjectKeyLabel:    lsinstallation.ClusterEndpointExportName,
					lsv1alpha1.DataObjectSourceLabel: fmt.Sprintf("Inst.%s", installation.Name),
				},
			},
			Data: utils.StringToAnyJSON(clusterEndpoint),
		}
		Expect(testenv.Client.Create(ctx, endpointExport)).To(Succeed())

		clusterKubeConfig := "dummy"
		kubeconfigExport := &lsv1alpha1.DataObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubeconfigexport",
				Namespace: state.Namespace,
				Labels: map[string]string{
					lsv1alpha1.DataObjectKeyLabel:    lsinstallation.ClusterKubeconfigExportName,
					lsv1alpha1.DataObjectSourceLabel: fmt.Sprintf("Inst.%s", installation.Name),
				},
			},
			Data: utils.StringToAnyJSON(clusterKubeConfig),
		}
		Expect(testenv.Client.Create(ctx, kubeconfigExport)).To(Succeed())

		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())

		Expect(instance.Status.ClusterEndpoint).To(Equal(clusterEndpoint))
		Expect(instance.Status.ClusterKubeconfig).To(Equal(clusterKubeConfig))
	})

	It("should create registry pull secrets for the context", func() {
		var err error
		state, err = testenv.InitResources(ctx, "./testdata/reconcile/test3")
		Expect(err).ToNot(HaveOccurred())

		op.Config().LandscaperServiceComponent.RegistryPullSecrets = []corev1.SecretReference{
			{
				Name:      "regpullsecret1",
				Namespace: state.Namespace,
			},
			{
				Name:      "regpullsecret2",
				Namespace: state.Namespace,
			},
		}

		instance := state.GetInstance("test")
		configuredSecret1 := state.GetSecret("regpullsecret1")
		configuredSecret2 := state.GetSecret("regpullsecret2")

		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())
		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())

		context := &lsv1alpha1.Context{}
		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: instance.Status.ContextRef.Name, Namespace: instance.Status.ContextRef.Namespace}, context)).To(Succeed())
		Expect(context.RegistryPullSecrets).To(HaveLen(2))

		contextSecret := &corev1.Secret{}
		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: context.RegistryPullSecrets[0].Name, Namespace: state.Namespace}, contextSecret)).To(Succeed())
		Expect(contextSecret.Type).To(Equal(configuredSecret1.Type))
		Expect(contextSecret.Data).To(Equal(configuredSecret1.Data))

		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: context.RegistryPullSecrets[1].Name, Namespace: state.Namespace}, contextSecret)).To(Succeed())
		Expect(contextSecret.Type).To(Equal(configuredSecret2.Type))
		Expect(contextSecret.Data).To(Equal(configuredSecret2.Data))
	})

	It("should set the reconcile operation annotation when the spec has changed", func() {
		var err error
		state, err = testenv.InitResources(ctx, "./testdata/reconcile/test2")
		Expect(err).ToNot(HaveOccurred())

		instance := state.GetInstance("test")

		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())
		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())

		Expect(instance.Status.InstallationRef).ToNot(BeNil())

		installation := &lsv1alpha1.Installation{}
		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: instance.Status.InstallationRef.Name, Namespace: instance.Status.InstallationRef.Namespace}, installation)).To(Succeed())
		Expect(installation.Annotations).ToNot(BeNil())
		Expect(installation.Annotations).To(HaveKey(lsv1alpha1.OperationAnnotation))
		Expect(installation.Annotations[lsv1alpha1.OperationAnnotation]).To(Equal(string(lsv1alpha1.ReconcileOperation)))

		delete(installation.Annotations, lsv1alpha1.OperationAnnotation)
		Expect(testenv.Client.Update(ctx, installation)).To(Succeed())

		instance.Spec.LandscaperConfiguration.Deployers = append(instance.Spec.LandscaperConfiguration.Deployers, "mock")
		Expect(testenv.Client.Update(ctx, instance)).To(Succeed())
		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())

		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: installation.Name, Namespace: installation.Namespace}, installation)).To(Succeed())
		Expect(installation.Annotations).ToNot(BeNil())
		Expect(installation.Annotations).To(HaveKey(lsv1alpha1.OperationAnnotation))
		Expect(installation.Annotations[lsv1alpha1.OperationAnnotation]).To(Equal(string(lsv1alpha1.ReconcileOperation)))
	})

	It("should not set the reconcile operation annotation when the spec has not changed", func() {
		var err error
		state, err = testenv.InitResources(ctx, "./testdata/reconcile/test2")
		Expect(err).ToNot(HaveOccurred())

		instance := state.GetInstance("test")

		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())
		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())

		Expect(instance.Status.InstallationRef).ToNot(BeNil())

		installation := &lsv1alpha1.Installation{}
		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: instance.Status.InstallationRef.Name, Namespace: instance.Status.InstallationRef.Namespace}, installation)).To(Succeed())
		Expect(installation.Annotations).ToNot(BeNil())
		Expect(installation.Annotations).To(HaveKey(lsv1alpha1.OperationAnnotation))
		Expect(installation.Annotations[lsv1alpha1.OperationAnnotation]).To(Equal(string(lsv1alpha1.ReconcileOperation)))

		delete(installation.Annotations, lsv1alpha1.OperationAnnotation)
		Expect(testenv.Client.Update(ctx, installation)).To(Succeed())

		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())

		Expect(testenv.Client.Get(ctx, types.NamespacedName{Name: installation.Name, Namespace: installation.Namespace}, installation)).To(Succeed())
		Expect(installation.Annotations).To(BeNil())
	})

	It("should handle reconcile errors", func() {
		var (
			err       error
			operation = "Reconcile"
			reason    = "failed to reconcile"
			message   = "error message"
		)

		state, err = testenv.InitResources(ctx, "./testdata/reconcile/test3")
		Expect(err).ToNot(HaveOccurred())

		instance := state.GetInstance("test")

		ctrl.ReconcileFunc = func(ctx context.Context, deployment *lssv1alpha1.Instance) error {
			return lsserrors.NewWrappedError(fmt.Errorf(reason), operation, reason, message)
		}

		testutils.ShouldReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())
		testutils.ShouldNotReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())

		Expect(instance.Status.LastError).ToNot(BeNil())
		Expect(instance.Status.LastError.Operation).To(Equal(operation))
		Expect(instance.Status.LastError.Reason).To(Equal(reason))
		Expect(instance.Status.LastError.Message).To(Equal(message))
		Expect(instance.Status.LastError.LastUpdateTime.Time).Should(BeTemporally("==", instance.Status.LastError.LastTransitionTime.Time))

		time.Sleep(2 * time.Second)

		message = "error message updated"

		testutils.ShouldNotReconcile(ctx, ctrl, testutils.RequestFromObject(instance))
		Expect(testenv.Client.Get(ctx, kutil.ObjectKeyFromObject(instance), instance)).To(Succeed())

		Expect(instance.Status.LastError).ToNot(BeNil())
		Expect(instance.Status.LastError.Operation).To(Equal(operation))
		Expect(instance.Status.LastError.Reason).To(Equal(reason))
		Expect(instance.Status.LastError.Message).To(Equal(message))
		Expect(instance.Status.LastError.LastUpdateTime.Time).Should(BeTemporally(">", instance.Status.LastError.LastTransitionTime.Time))
	})
})
