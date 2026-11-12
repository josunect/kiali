// Assisted-by: Codex

package business

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kiali/kiali/config"
	"github.com/kiali/kiali/kubernetes"
	"github.com/kiali/kiali/kubernetes/kubetest"
	"github.com/kiali/kiali/models"
)

func TestWorkloadListWaypointServiceLookupReuse(t *testing.T) {
	for _, weight := range []string{"", "0", "25", "100"} {
		t.Run("canaryWeight="+weight, func(t *testing.T) {
			conf := config.NewConfig()
			conf.KubernetesConfig.ClusterName = "cluster-default"
			conf.ExternalServices.Istio.IstioAPIEnabled = false
			config.Set(conf)
			cluster := conf.KubernetesConfig.ClusterName
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{config.WaypointUseLabel: "primary"},
				Name:   "bookinfo",
			}}
			if weight != "" {
				ns.Labels[config.WaypointCanaryUseLabel] = "canary"
				ns.Annotations = map[string]string{config.WaypointCanaryWeightAnnotation: weight}
			}
			const count = 6
			objects := []runtime.Object{ns}
			for i := 0; i < count; i++ {
				name := fmt.Sprintf("app-%d", i)
				objects = append(objects,
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Annotations: kubetest.FakeIstioAmbientAnnotations(), Labels: map[string]string{"app": name}, Name: name, Namespace: ns.Name},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Image: "example", Name: name}}},
					},
					&corev1.Service{
						ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name},
						Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": name}},
					},
				)
			}
			layer, reader := newWaypointEnrollmentTestLayer(t, conf, objects...)
			waypoints := models.Workloads{}
			for _, name := range []string{"primary", "canary"} {
				waypoints = append(waypoints, &models.Workload{WorkloadListItem: models.WorkloadListItem{
					Cluster:   cluster,
					Labels:    map[string]string{config.WaypointLabel: config.WaypointLabelValue, config.WaypointFor: config.WaypointForService},
					Name:      name,
					Namespace: ns.Name,
				}})
			}
			layer.Workload.cache.SetWaypoints(waypoints)
			list, err := layer.Workload.GetWorkloadList(context.Background(), WorkloadCriteria{Cluster: cluster, Namespace: ns.Name})
			require.NoError(t, err)
			require.Len(t, list.Workloads, count)
			// Each workload's Service selector requires one namespace scan. Resolving
			// its canary must not perform a second scan after capture is established.
			assert.Equal(t, int64(count*count), reader.serviceItemsRead.Load())
			expected := []models.WorkloadReferenceInfo{{Cluster: cluster, Name: "primary", Namespace: ns.Name, Type: config.WaypointForService, WaypointRole: models.WaypointRolePrimary}}
			if weight != "" {
				parsed, err := strconv.Atoi(weight)
				require.NoError(t, err)
				expected = append(expected, models.WorkloadReferenceInfo{CanaryWeight: parsed, Cluster: cluster, Name: "canary", Namespace: ns.Name, RoleWeight: &parsed, Type: config.WaypointForService, WaypointRole: models.WaypointRoleCanary})
			}
			for _, workload := range list.Workloads {
				assert.True(t, workload.IsAmbient, workload.Name)
				refs := layer.Workload.getWaypointsForWorkload(context.Background(), models.Workload{WorkloadListItem: workload}, false, waypoints)
				assert.Equal(t, expected, refs, workload.Name)
			}
		})
	}
}

func TestWorkloadWaypointLookupPreservesOptOut(t *testing.T) {
	for _, level := range []string{"namespace", "service", "pod"} {
		t.Run(level, func(t *testing.T) {
			conf := config.NewConfig()
			conf.KubernetesConfig.ClusterName = "cluster-default"
			conf.ExternalServices.Istio.IstioAPIEnabled = false
			config.Set(conf)
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{config.WaypointCanaryWeightAnnotation: "25"},
				Labels:      map[string]string{config.WaypointUseLabel: "primary", config.WaypointCanaryUseLabel: "canary"},
				Name:        "bookinfo",
			}}
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}, Name: "reviews", Namespace: ns.Name},
				Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "reviews"}},
			}
			workload := models.Workload{WorkloadListItem: models.WorkloadListItem{
				Cluster:     conf.KubernetesConfig.ClusterName,
				Labels:      map[string]string{"app": "reviews"},
				Name:        "reviews",
				Namespace:   ns.Name,
				WorkloadGVK: kubernetes.Pods,
			}}
			switch level {
			case "namespace":
				ns.Labels[config.WaypointUseLabel] = config.WaypointNone
			case "service":
				svc.Labels[config.WaypointUseLabel] = config.WaypointNone
			case "pod":
				workload.Labels[config.WaypointUseLabel] = config.WaypointNone
			}
			layer, _ := newWaypointEnrollmentTestLayer(t, conf, ns, svc)
			ctx := context.Background()
			waypoints := models.Workloads{&models.Workload{WorkloadListItem: models.WorkloadListItem{
				Cluster:   workload.Cluster,
				Labels:    map[string]string{config.WaypointLabel: config.WaypointLabelValue},
				Name:      "canary",
				Namespace: ns.Name,
			}}}
			assert.Empty(t, layer.Workload.getWaypointsForWorkload(ctx, workload, false, waypoints))
			assert.Nil(t, waypointTrafficSplitForWorkload(ctx, &layer.Workload, workload))
		})
	}
}
