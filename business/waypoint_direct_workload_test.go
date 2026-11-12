// Assisted-by: Codex

package business

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apps_v1 "k8s.io/api/apps/v1"
	core_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kiali/kiali/config"
	"github.com/kiali/kiali/kubernetes"
	"github.com/kiali/kiali/kubernetes/kubetest"
	"github.com/kiali/kiali/models"
)

// Standalone Pods have no service-addressed path. A namespace canary cannot capture them.
func TestDirectWorkloadDoesNotInheritCanary(t *testing.T) {
	for _, podOverride := range []bool{false, true} {
		for _, weight := range []string{"", "0", "25", "100"} {
			t.Run(fmt.Sprintf("override=%t/weight=%s", podOverride, weight), func(t *testing.T) {
				conf := config.NewConfig()
				conf.KubernetesConfig.ClusterName = "cluster-default"
				conf.ExternalServices.Istio.IstioAPIEnabled = false
				conf.ExternalServices.CustomDashboards.Enabled = false
				config.Set(conf)
				ns := &core_v1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{config.WaypointCanaryWeightAnnotation: weight},
					Labels: map[string]string{
						config.IstioAmbientNamespaceLabel: config.IstioAmbientNamespaceLabelValue,
						config.WaypointCanaryUseLabel:     "canary",
						config.WaypointUseLabel:           "primary",
					},
					Name: "bookinfo",
				}}
				if weight == "" {
					delete(ns.Labels, config.WaypointCanaryUseLabel)
					ns.Annotations = nil
				}
				pod := &core_v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: kubetest.FakeIstioAmbientAnnotations(), Labels: map[string]string{"app": "direct"}, Name: "direct-pod", Namespace: ns.Name}, Spec: core_v1.PodSpec{Containers: []core_v1.Container{{Image: "test", Name: "app"}}}}
				want := "primary"
				if podOverride {
					pod.Labels[config.WaypointUseLabel] = "own"
					want = "own"
				}
				ztunnel := &apps_v1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{config.KubernetesAppLabel: config.Ztunnel}, Name: "ztunnel", Namespace: ns.Name}}
				layer := NewLayerBuilder(t, conf).WithClient(kubetest.NewFakeK8sClient(ns, pod, ztunnel)).Build()
				waypoints := models.Workloads{}
				for _, name := range []string{"primary", "canary", "own"} {
					waypoints = append(waypoints, &models.Workload{WorkloadListItem: models.WorkloadListItem{
						Cluster: conf.KubernetesConfig.ClusterName, Labels: map[string]string{config.WaypointFor: config.WaypointForAll, config.WaypointLabel: config.WaypointLabelValue}, Name: name, Namespace: ns.Name,
					}})
				}
				layer.Workload.cache.SetWaypoints(waypoints)
				detail, err := layer.Workload.GetWorkload(context.Background(), WorkloadCriteria{Cluster: conf.KubernetesConfig.ClusterName, IncludeServices: true, Namespace: ns.Name, WorkloadGVK: kubernetes.Pods, WorkloadName: pod.Name})
				require.NoError(t, err)
				require.Empty(t, detail.Services)
				got := []string{}
				for _, ref := range detail.WaypointWorkloads {
					got = append(got, ref.Name)
				}
				t.Logf("pod=%s selected services=%d waypoint refs=%v; direct primary=%s", pod.Name, len(detail.Services), got, want)
				assert.Equal(t, []string{want}, got)
				assert.Empty(t, detail.WaypointTrafficSplitConfigs, "direct workload traffic has no Service canary split")
			})
		}
	}
}

func TestServiceBackedWorkloadRetainsCanary(t *testing.T) {
	for _, tc := range []struct {
		annotations     map[string]string
		canary          string
		canaryNamespace string
		labels          map[string]string
		name            string
		weight          int
	}{
		{canary: "canary", canaryNamespace: "bookinfo", name: "namespace inheritance", weight: 25},
		{annotations: map[string]string{config.WaypointCanaryWeightAnnotation: "0"}, canary: "canary", canaryNamespace: "bookinfo", name: "zero override", weight: 0},
		{annotations: map[string]string{config.WaypointCanaryWeightAnnotation: "100"}, canary: "canary", canaryNamespace: "bookinfo", name: "full override", weight: 100},
		{annotations: map[string]string{config.WaypointCanaryWeightAnnotation: "80"}, canary: "service-canary", canaryNamespace: "infra", labels: map[string]string{config.WaypointCanaryUseLabel: "service-canary", config.WaypointCanaryNamespaceLabel: "infra"}, name: "cross namespace override", weight: 80},
		{labels: map[string]string{config.WaypointCanaryUseLabel: config.WaypointNone}, name: "canary optout"},
		{labels: map[string]string{config.WaypointUseLabel: "primary"}, name: "explicit Service primary stops inheritance"},
	} {
		for _, includeServices := range []bool{false, true} {
			for _, podOverride := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/includeServices=%t/podOverride=%t", tc.name, includeServices, podOverride), func(t *testing.T) {
					conf := config.NewConfig()
					conf.KubernetesConfig.ClusterName = "cluster-default"
					conf.ExternalServices.Istio.IstioAPIEnabled = false
					conf.ExternalServices.CustomDashboards.Enabled = false
					config.Set(conf)
					cluster := conf.KubernetesConfig.ClusterName
					ns := &core_v1.Namespace{ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{config.WaypointCanaryWeightAnnotation: "25"},
						Labels:      map[string]string{config.IstioAmbientNamespaceLabel: config.IstioAmbientNamespaceLabelValue, config.WaypointUseLabel: "primary", config.WaypointCanaryUseLabel: "canary"},
						Name:        "bookinfo",
					}}
					pod := &core_v1.Pod{
						ObjectMeta: metav1.ObjectMeta{Annotations: kubetest.FakeIstioAmbientAnnotations(), Labels: map[string]string{"app": "reviews"}, Name: "reviews", Namespace: ns.Name},
						Spec:       core_v1.PodSpec{Containers: []core_v1.Container{{Image: "test", Name: "app"}}},
					}
					primary := "primary"
					if podOverride {
						pod.Labels[config.WaypointUseLabel] = "own"
						primary = "own"
					}
					svc := &core_v1.Service{
						ObjectMeta: metav1.ObjectMeta{Annotations: tc.annotations, Labels: tc.labels, Name: "reviews", Namespace: ns.Name},
						Spec:       core_v1.ServiceSpec{Selector: map[string]string{"app": "reviews"}},
					}
					ztunnel := &apps_v1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{config.KubernetesAppLabel: config.Ztunnel}, Name: "ztunnel", Namespace: ns.Name}}
					layer, reader := newWaypointEnrollmentTestLayer(t, conf, ns, kubetest.FakeNamespace("infra"), pod, svc, ztunnel)
					waypoints := models.Workloads{}
					for _, name := range []string{"primary", "own", "canary", "service-canary"} {
						namespace := ns.Name
						if name == "service-canary" {
							namespace = "infra"
						}
						waypoints = append(waypoints, &models.Workload{WorkloadListItem: models.WorkloadListItem{
							Cluster:   cluster,
							Labels:    map[string]string{config.WaypointFor: config.WaypointForAll, config.WaypointLabel: config.WaypointLabelValue},
							Name:      name,
							Namespace: namespace,
						}})
					}
					layer.Workload.cache.SetWaypoints(waypoints)
					detail, err := layer.Workload.GetWorkload(context.Background(), WorkloadCriteria{Cluster: cluster, IncludeServices: includeServices, Namespace: ns.Name, WorkloadGVK: kubernetes.Pods, WorkloadName: pod.Name})
					require.NoError(t, err)
					require.NotEmpty(t, detail.WaypointWorkloads)
					assert.Equal(t, primary, detail.WaypointWorkloads[0].Name)
					if podOverride && !includeServices {
						// A direct binding alone cannot establish a Service relationship.
						// Keep its fast path; requesting Services below resolves that path.
						assert.Len(t, detail.WaypointWorkloads, 1)
						assert.Empty(t, detail.WaypointTrafficSplitConfigs)
						assert.Empty(t, detail.Services)
						assert.Zero(t, reader.serviceItemsRead.Load())
						return
					}
					require.Len(t, detail.WaypointTrafficSplitConfigs, 1)
					cfg := detail.WaypointTrafficSplitConfigs[0]
					if len(tc.labels) > 0 || len(tc.annotations) > 0 {
						assert.Equal(t, "service", cfg.ConfigLevel)
						assert.Equal(t, svc.Name, cfg.ServiceName)
					}
					if tc.canary == "" {
						assert.Len(t, detail.WaypointWorkloads, 1)
						assert.Nil(t, cfg.Split.Canary)
					} else {
						require.Len(t, detail.WaypointWorkloads, 2)
						ref := detail.WaypointWorkloads[1]
						assert.Equal(t, tc.canary, ref.Name)
						assert.Equal(t, tc.canaryNamespace, ref.Namespace)
						assert.Equal(t, models.WaypointRoleCanary, ref.WaypointRole)
						require.NotNil(t, ref.RoleWeight)
						assert.Equal(t, tc.weight, *ref.RoleWeight)
						assert.Equal(t, tc.weight, cfg.Split.CanaryWeight)
					}
					if includeServices {
						require.Len(t, detail.Services, 1)
					} else {
						assert.Empty(t, detail.Services, "resolving a split must not attach unrequested Services")
					}
				})
			}
		}
	}
}

func TestDirectWaypointBindingDoesNotScanServices(t *testing.T) {
	conf := config.NewConfig()
	conf.KubernetesConfig.ClusterName = "cluster-default"
	conf.ExternalServices.Istio.IstioAPIEnabled = false
	config.Set(conf)
	ns := &core_v1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{config.WaypointCanaryWeightAnnotation: "25"},
		Labels:      map[string]string{config.WaypointUseLabel: "primary", config.WaypointCanaryUseLabel: "canary"}, Name: "bookinfo",
	}}
	svc := &core_v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: ns.Name}}
	layer, reader := newWaypointEnrollmentTestLayer(t, conf, ns, svc)
	for _, gvk := range []schema.GroupVersionKind{kubernetes.Pods, kubernetes.Deployments, kubernetes.WorkloadGroups} {
		workload := models.Workload{
			WorkloadListItem: models.WorkloadListItem{Cluster: conf.KubernetesConfig.ClusterName, Labels: map[string]string{"app": "direct", config.WaypointUseLabel: "own"}, Namespace: ns.Name, TemplateLabels: map[string]string{config.WaypointUseLabel: "own"}, WorkloadGVK: gvk},
		}
		split, serviceName := waypointTrafficSplitForWorkload(context.Background(), &layer.Workload, workload)
		assert.Nil(t, split)
		assert.Empty(t, serviceName)
	}
	assert.Zero(t, reader.serviceItemsRead.Load(), "direct bindings must retain their no-Service-scan fast path")
}
