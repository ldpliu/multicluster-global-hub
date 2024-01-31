package backup_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	mchv1 "github.com/stolostron/multiclusterhub-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	mchName      = "mch"
	mchNamespace = "open-cluster-management"
	pvcName      = "postpvc"
)

var pvcNamespace = constants.GHDefaultNamespace
var mchObj = &mchv1.MultiClusterHub{
	ObjectMeta: metav1.ObjectMeta{
		Name:      mchName,
		Namespace: mchNamespace,
	},
	Spec: mchv1.MultiClusterHubSpec{
		Overrides: &mchv1.Overrides{
			Components: []mchv1.ComponentConfig{
				{
					Name:    "cluster-backup",
					Enabled: true,
				},
			},
		},
	},
}
var postgresPvc = &corev1.PersistentVolumeClaim{
	ObjectMeta: v1.ObjectMeta{
		Name:      pvcName,
		Namespace: pvcNamespace,
	},
	Spec: corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("5Gi"),
			},
		},
	},
}
var _ = Describe("backup pvc", Ordered, func() {
	BeforeAll(func() {
		Expect(mgr.GetClient().Create(ctx, postgresPvc)).NotTo(HaveOccurred())
		Expect(mgr.GetClient().Create(ctx, mchObj)).Should(Succeed())
	})

	It("update pvc which do not need backup", func() {
		Eventually(func() error {
			postgresPvc := &corev1.PersistentVolumeClaim{}
			err := mgr.GetClient().Get(ctx, types.NamespacedName{
				Namespace: pvcNamespace,
				Name:      pvcName,
			}, postgresPvc)
			if err != nil {
				return err
			}
			postgresPvc.Labels = map[string]string{
				"comonents":               "postgres",
				constants.BackupVolumnKey: constants.BackupGlobalHubValue,
			}
			err = mgr.GetClient().Update(ctx, postgresPvc)
			return err
		}, timeout, interval).Should(Succeed())
	})

	It("update pvc which need backup", func() {
		Eventually(func() error {
			postgresPvc := &corev1.PersistentVolumeClaim{}
			err := mgr.GetClient().Get(ctx, types.NamespacedName{
				Namespace: pvcNamespace,
				Name:      pvcName,
			}, postgresPvc)
			if err != nil {
				return err
			}
			postgresPvc.Labels = map[string]string{
				"comonents":                     "postgres",
				constants.BackupVolumnKey:       constants.BackupGlobalHubValue,
				constants.BackupPvcLastSchedule: "now",
			}
			err = mgr.GetClient().Update(ctx, postgresPvc)
			if err != nil {
				return err
			}
			return err
		}, timeout, interval).Should(Succeed())

		Eventually(func() error {
			postgresPvc := &corev1.PersistentVolumeClaim{}
			err := mgr.GetClient().Get(ctx, types.NamespacedName{
				Namespace: pvcNamespace,
				Name:      pvcName,
			}, postgresPvc)
			if err != nil {
				return err
			}
			postgresPvc.Labels = map[string]string{
				"comonents":                     "postgres",
				constants.BackupVolumnKey:       constants.BackupGlobalHubValue,
				constants.BackupPvcLastSchedule: "now",
				constants.BackupPvcLastHookName: "now",
			}
			err = mgr.GetClient().Update(ctx, postgresPvc)
			if err != nil {
				return err
			}
			return err
		}, timeout, interval).Should(Succeed())
		backupReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: pvcNamespace,
				Name:      pvcName,
			}})
	})

	It("disable mch and pvc do not need backup", func() {
		Eventually(func() error {
			mch := &mchv1.MultiClusterHub{}
			err := mgr.GetClient().Get(ctx, types.NamespacedName{
				Namespace: mchObj.Namespace,
				Name:      mchObj.Name,
			}, mch)
			if err != nil {
				return err
			}
			mch.Spec = mchv1.MultiClusterHubSpec{}
			err = mgr.GetClient().Update(ctx, mch)
			if err != nil {
				return err
			}
			return err
		}, timeout, interval).Should(Succeed())
	})
})
