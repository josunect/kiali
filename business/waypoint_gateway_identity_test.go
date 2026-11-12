// Assisted-by: Codex

package business

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apps_v1 "k8s.io/api/apps/v1"
	core_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kiali/kiali/config"
	"github.com/kiali/kiali/kubernetes"
	"github.com/kiali/kiali/kubernetes/kubetest"
	"github.com/kiali/kiali/models"
)

func TestGatewaySplitConfigsRespectNamespaceIdentity(t *testing.T) {
	for _, primaryNamespace := range []string{"source", "infra"} {
		for _, canaryNamespace := range []string{"source", "infra"} {
			for _, weight := range []string{"", "0", "25", "100"} {
				for _, serviceOverride := range []bool{false, true} {
					name := fmt.Sprintf("primary=%s/canary=%s/weight=%s/service=%t", primaryNamespace, canaryNamespace, weight, serviceOverride)
					t.Run(name, func(t *testing.T) {
						conf := config.NewConfig()
						conf.KubernetesConfig.ClusterName = "cluster-default"
						conf.ExternalServices.CustomDashboards.Enabled = false
						conf.ExternalServices.Istio.IstioAPIEnabled = false
						config.Set(conf)
						cluster := conf.KubernetesConfig.ClusterName
						source := &core_v1.Namespace{ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{},
							Labels:      map[string]string{config.WaypointUseLabel: "waypoint"},
							Name:        "source",
						}}
						if primaryNamespace != source.Name {
							source.Labels[config.WaypointUseNamespaceLabel] = primaryNamespace
						}
						if weight != "" {
							source.Labels[config.WaypointCanaryUseLabel] = "waypoint-canary"
							if canaryNamespace != source.Name {
								source.Labels[config.WaypointCanaryNamespaceLabel] = canaryNamespace
							}
							source.Annotations[config.WaypointCanaryWeightAnnotation] = weight
						}
						objects := []runtime.Object{source, kubetest.FakeNamespace("infra")}
						if serviceOverride {
							serviceLabels := map[string]string{}
							if weight == "" {
								serviceLabels[config.WaypointCanaryUseLabel] = "waypoint-canary"
								serviceLabels[config.WaypointCanaryNamespaceLabel] = canaryNamespace
							}
							objects = append(objects, &core_v1.Service{ObjectMeta: metav1.ObjectMeta{
								Annotations: map[string]string{config.WaypointCanaryWeightAnnotation: "80"},
								Labels:      serviceLabels,
								Name:        "reviews",
								Namespace:   source.Name,
							}})
						}
						waypoints := models.Workloads{}
						// Both namespaces contain identically named gateways. Only the
						// gateway identities in the bindings may receive split configs.
						for _, namespace := range []string{source.Name, "infra"} {
							for _, gateway := range []string{"waypoint", "waypoint-canary"} {
								labels := map[string]string{"app": gateway, config.WaypointFor: config.WaypointForAll, config.WaypointLabel: config.WaypointLabelValue}
								objects = append(objects, &apps_v1.Deployment{
									ObjectMeta: metav1.ObjectMeta{Name: gateway, Namespace: namespace},
									Spec: apps_v1.DeploymentSpec{
										Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": gateway}},
										Template: core_v1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}},
									},
								})
								waypoints = append(waypoints, &models.Workload{WorkloadListItem: models.WorkloadListItem{Cluster: cluster, Labels: labels, Name: gateway, Namespace: namespace}})
							}
						}
						layer := NewLayerBuilder(t, conf).WithClient(kubetest.NewFakeK8sClient(objects...)).Build()
						layer.Workload.cache.SetWaypoints(waypoints)
						for _, gateway := range waypoints {
							detail, err := layer.Workload.GetWorkload(context.Background(), WorkloadCriteria{Cluster: cluster, Namespace: gateway.Namespace, WorkloadGVK: kubernetes.Deployments, WorkloadName: gateway.Name})
							require.NoError(t, err)
							require.True(t, detail.IsWaypoint)
							expectedCount := 0
							matches := gateway.Name == "waypoint" && gateway.Namespace == primaryNamespace || gateway.Name == "waypoint-canary" && gateway.Namespace == canaryNamespace
							if matches {
								if weight != "" {
									expectedCount++
								}
								if serviceOverride {
									expectedCount++
								}
							}
							require.Len(t, detail.WaypointTrafficSplitConfigs, expectedCount, "%s/%s", gateway.Namespace, gateway.Name)
							for _, cfg := range detail.WaypointTrafficSplitConfigs {
								require.NotNil(t, cfg.Split)
								require.NotNil(t, cfg.Split.Canary)
								assert.Equal(t, source.Name, cfg.Namespace)
								assert.Equal(t, models.WaypointBinding{Cluster: cluster, Name: "waypoint", Namespace: primaryNamespace, Role: models.WaypointRolePrimary}, cfg.Split.Primary)
								assert.Equal(t, models.WaypointBinding{Cluster: cluster, Name: "waypoint-canary", Namespace: canaryNamespace, Role: models.WaypointRoleCanary}, *cfg.Split.Canary)
								expectedWeight := 80
								if cfg.ConfigLevel == "namespace" {
									expectedWeight, err = strconv.Atoi(weight)
									require.NoError(t, err)
									assert.Empty(t, cfg.ServiceName)
								} else {
									assert.Equal(t, "service", cfg.ConfigLevel)
									assert.Equal(t, "reviews", cfg.ServiceName)
								}
								assert.Equal(t, expectedWeight, cfg.Split.CanaryWeight)
								assert.Equal(t, 100-expectedWeight, cfg.Split.PrimaryWeight)
							}
						}
					})
				}
			}
		}
	}
}
