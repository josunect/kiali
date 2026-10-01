// Package tracing implements an experimental TelemetryVendor that builds a TrafficMap
// from Jaeger/Tempo traces instead of Prometheus metrics.
//
// Select it with telemetryVendor=tracing. It is not an appender: appenders enrich an
// already-built metrics graph; this vendor replaces the telemetry source.
package tracing

import (
	"context"
	"time"

	"github.com/kiali/kiali/business"
	"github.com/kiali/kiali/config"
	"github.com/kiali/kiali/graph"
	"github.com/kiali/kiali/graph/telemetry"
	"github.com/kiali/kiali/log"
	"github.com/kiali/kiali/models"
	"github.com/kiali/kiali/observability"
	jaegerModels "github.com/kiali/kiali/tracing/jaeger/model/json"
)

const (
	// defaultTraceLimitPerApp caps traces fetched per app to keep graph generation bounded.
	defaultTraceLimitPerApp = 20
	// maxAppsPerNamespace caps how many apps are queried per namespace.
	maxAppsPerNamespace = 50
)

// GlobalTracingInfo is the vendor-specific cache for a single tracing graph request.
type GlobalTracingInfo struct{}

type (
	GlobalInfo = graph.GlobalInfo[*GlobalTracingInfo]
)

// NewGlobalTracingInfo constructs empty vendor info for a tracing graph request.
func NewGlobalTracingInfo() *GlobalTracingInfo {
	return &GlobalTracingInfo{}
}

// BuildNamespacesTrafficMap builds a TrafficMap by fetching traces for apps in the
// requested namespaces and aggregating parent→child span edges.
func BuildNamespacesTrafficMap(ctx context.Context, o graph.TelemetryOptions, globalInfo *GlobalInfo) graph.TrafficMap {
	ctx, end := observability.StartSpan(
		ctx,
		"BuildNamespacesTrafficMap",
		observability.Attribute("package", "tracing"),
	)
	defer end()

	zl := log.FromContext(ctx)
	zl.Trace().Msgf("Build tracing [%s] graph for [%d] namespaces [%v]", o.GraphType, len(o.Namespaces), o.Namespaces)

	if globalInfo.Business == nil {
		graph.Error("Tracing graph requires business layer")
	}
	if !globalInfo.Conf.ExternalServices.Tracing.Enabled {
		zl.Warn().Msg("Tracing is disabled; returning empty traffic map")
		return graph.NewTrafficMap()
	}

	clusterName := config.Get().KubernetesConfig.ClusterName
	if clusterName == "" {
		clusterName = config.DefaultClusterID
	}

	trafficMap := graph.NewTrafficMap()
	queryTime := time.Unix(o.QueryTime, 0)

	for _, nsInfo := range o.Namespaces {
		nsTraces := fetchNamespaceTraces(ctx, globalInfo.Business, nsInfo, queryTime, clusterName)
		nsMap := BuildTrafficMapFromTraces(nsTraces, BuildOptions{
			Cluster:          clusterName,
			DefaultNamespace: nsInfo.Name,
			GraphType:        o.GraphType,
		})
		telemetry.MergeTrafficMaps(trafficMap, nsInfo.Name, nsMap)
	}

	return trafficMap
}

// BuildNodeTrafficMap builds a node-detail graph from traces involving the requested node.
func BuildNodeTrafficMap(ctx context.Context, o graph.TelemetryOptions, globalInfo *GlobalInfo) (graph.TrafficMap, error) {
	ctx, end := observability.StartSpan(
		ctx,
		"BuildNodeTrafficMap",
		observability.Attribute("package", "tracing"),
	)
	defer end()

	// Reuse namespace aggregation then filter to the requested node neighbourhood.
	full := BuildNamespacesTrafficMap(ctx, o, globalInfo)
	return filterNodeNeighborhood(full, o), nil
}

// GraphOptionsMatch reports whether two option sets would produce the same tracing traffic map.
func GraphOptionsMatch(a, b graph.TelemetryOptions) bool {
	// Tracing graphs currently depend only on shared TelemetryOptions fields already
	// compared by the cache layer (namespaces, duration, graphType, queryTime).
	_ = a
	_ = b
	return true
}

func fetchNamespaceTraces(ctx context.Context, layer *business.Layer, nsInfo graph.NamespaceInfo, queryTime time.Time, clusterName string) []jaegerModels.Trace {
	zl := log.FromContext(ctx)

	apps, err := layer.App.GetAppList(ctx, business.AppCriteria{
		Namespace: nsInfo.Name,
		Cluster:   clusterName,
	})
	if err != nil {
		zl.Warn().Err(err).Msgf("Failed to list apps in namespace [%s] for tracing graph", nsInfo.Name)
		return nil
	}

	query := models.TracingQuery{
		Start:   queryTime.Add(-nsInfo.Duration),
		End:     queryTime,
		Limit:   defaultTraceLimitPerApp,
		Tags:    map[string]string{},
		Cluster: clusterName,
	}

	seen := map[string]struct{}{}
	var traces []jaegerModels.Trace
	count := 0
	for _, app := range apps.Apps {
		if count >= maxAppsPerNamespace {
			break
		}
		count++
		r, err := layer.Tracing.GetAppTraces(ctx, nsInfo.Name, app.Name, app.Name, query)
		if err != nil {
			zl.Debug().Err(err).Msgf("No traces for app [%s] in [%s]", app.Name, nsInfo.Name)
			continue
		}
		if r == nil {
			continue
		}
		for _, tr := range r.Data {
			id := string(tr.TraceID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			traces = append(traces, tr)
		}
	}

	zl.Trace().Msgf("Fetched [%d] unique traces for namespace [%s] from [%d] apps", len(traces), nsInfo.Name, count)
	return traces
}

// filterNodeNeighborhood keeps the requested node and its direct neighbours.
func filterNodeNeighborhood(tm graph.TrafficMap, o graph.TelemetryOptions) graph.TrafficMap {
	if o.NodeOptions.App == "" && o.NodeOptions.Workload == "" && o.NodeOptions.Service == "" {
		return tm
	}

	targetIDs := map[string]bool{}
	for id, n := range tm {
		match := true
		if o.NodeOptions.App != "" && n.App != o.NodeOptions.App {
			match = false
		}
		if o.NodeOptions.Workload != "" && n.Workload != o.NodeOptions.Workload {
			match = false
		}
		if o.NodeOptions.Service != "" && n.Service != o.NodeOptions.Service {
			match = false
		}
		if o.NodeOptions.Namespace.Name != "" && n.Namespace != o.NodeOptions.Namespace.Name {
			match = false
		}
		if match {
			targetIDs[id] = true
		}
	}
	if len(targetIDs) == 0 {
		return graph.NewTrafficMap()
	}

	keep := map[string]bool{}
	for id := range targetIDs {
		keep[id] = true
	}
	// Include direct edge neighbours.
	for id, n := range tm {
		if targetIDs[id] {
			for _, e := range n.Edges {
				keep[e.Dest.ID] = true
			}
		} else {
			for _, e := range n.Edges {
				if targetIDs[e.Dest.ID] {
					keep[id] = true
				}
			}
		}
	}

	out := graph.NewTrafficMap()
	for id := range keep {
		if n, ok := tm[id]; ok {
			clone := *n
			clone.Edges = nil
			for _, e := range n.Edges {
				if keep[e.Dest.ID] {
					clone.Edges = append(clone.Edges, e)
				}
			}
			out[id] = &clone
		}
	}
	return out
}
