package tracing

import (
	"strings"

	"github.com/kiali/kiali/config"
	"github.com/kiali/kiali/graph"
	jaegerModels "github.com/kiali/kiali/tracing/jaeger/model/json"
)

// Metadata keys specific to the trace-based graph POC.
const (
	// TraceCallCount is the absolute number of parent→child calls observed across traces.
	// Unlike Prometheus-backed graphs this is a count, not a rate (rps).
	TraceCallCount graph.MetadataKey = "traceCallCount"
	// TraceIDs lists distinct trace IDs that contributed to an edge (capped).
	TraceIDs graph.MetadataKey = "traceIDs"
)

const maxTraceIDsPerEdge = 10

// BuildOptions controls how traces are mapped into graph nodes.
type BuildOptions struct {
	// Cluster is assigned to every node. Defaults to config.DefaultClusterID.
	Cluster string
	// GraphType must be one of graph.GraphTypeApp / VersionedApp / Workload / Service.
	// Defaults to graph.GraphTypeApp.
	GraphType string
	// DefaultNamespace is used when a process serviceName has no namespace suffix
	// (e.g. "productpage" instead of "productpage.bookinfo").
	DefaultNamespace string
}

// serviceIdentity is the minimal node identity we can extract from a span.
type serviceIdentity struct {
	App       string
	Namespace string
}

// BuildTrafficMapFromTraces aggregates parent→child service edges from one or more traces
// into a TrafficMap. Spans that share the same service identity do not create edges
// (intra-service client/server pairs are ignored).
func BuildTrafficMapFromTraces(traces []jaegerModels.Trace, opts BuildOptions) graph.TrafficMap {
	if opts.Cluster == "" {
		opts.Cluster = config.DefaultClusterID
	}
	if opts.GraphType == "" {
		opts.GraphType = graph.GraphTypeApp
	}
	if opts.DefaultNamespace == "" {
		opts.DefaultNamespace = "default"
	}

	trafficMap := graph.NewTrafficMap()
	// edgeKey → edge for aggregation across spans/traces
	type edgeAgg struct {
		edge     *graph.Edge
		count    float64
		traceIDs map[string]struct{}
	}
	edges := map[string]*edgeAgg{}

	for _, trace := range traces {
		byID := indexSpans(trace)
		for i := range trace.Spans {
			span := &trace.Spans[i]
			child := resolveIdentity(trace, span, opts.DefaultNamespace)
			if child.App == "" {
				continue
			}
			parentSpan := parentOf(byID, span)
			if parentSpan == nil {
				// Root spans become nodes but contribute no edge.
				ensureNode(trafficMap, opts, child)
				continue
			}
			parent := resolveIdentity(trace, parentSpan, opts.DefaultNamespace)
			if parent.App == "" {
				ensureNode(trafficMap, opts, child)
				continue
			}
			// Same service: client/server pair inside one process — skip edge.
			if parent.App == child.App && parent.Namespace == child.Namespace {
				ensureNode(trafficMap, opts, child)
				continue
			}

			src := ensureNode(trafficMap, opts, parent)
			dst := ensureNode(trafficMap, opts, child)
			if src == nil || dst == nil {
				continue
			}

			key := src.ID + "->" + dst.ID
			agg, ok := edges[key]
			if !ok {
				e := src.AddEdge(dst)
				agg = &edgeAgg{edge: e, traceIDs: map[string]struct{}{}}
				edges[key] = agg
			}
			agg.count++
			traceID := string(trace.TraceID)
			if traceID != "" && len(agg.traceIDs) < maxTraceIDsPerEdge {
				agg.traceIDs[traceID] = struct{}{}
			}
		}
	}

	for _, agg := range edges {
		// Populate rates + httpResponses the same way the Istio vendor does so
		// graph/config/common.addEdgeTelemetry can marshal the edge without panicking.
		graph.AddToMetadata(
			graph.HTTP.Name,
			agg.count,
			"200",
			"-",
			"",
			agg.edge.Source.Metadata,
			agg.edge.Dest.Metadata,
			agg.edge.Metadata,
		)
		agg.edge.Metadata[graph.ProtocolKey] = graph.HTTP.Name
		agg.edge.Metadata[TraceCallCount] = agg.count
		ids := make([]string, 0, len(agg.traceIDs))
		for id := range agg.traceIDs {
			ids = append(ids, id)
		}
		if len(ids) > 0 {
			agg.edge.Metadata[TraceIDs] = ids
		}
	}

	return trafficMap
}

func indexSpans(trace jaegerModels.Trace) map[jaegerModels.SpanID]*jaegerModels.Span {
	byID := make(map[jaegerModels.SpanID]*jaegerModels.Span, len(trace.Spans))
	for i := range trace.Spans {
		s := &trace.Spans[i]
		byID[s.SpanID] = s
	}
	return byID
}

func parentOf(byID map[jaegerModels.SpanID]*jaegerModels.Span, span *jaegerModels.Span) *jaegerModels.Span {
	for _, ref := range span.References {
		if ref.RefType == jaegerModels.ChildOf {
			if p, ok := byID[ref.SpanID]; ok {
				return p
			}
		}
	}
	if span.ParentSpanID != "" {
		if p, ok := byID[span.ParentSpanID]; ok {
			return p
		}
	}
	return nil
}

func resolveIdentity(trace jaegerModels.Trace, span *jaegerModels.Span, defaultNS string) serviceIdentity {
	svc := spanServiceName(trace, span)
	app, ns := splitServiceName(svc, defaultNS)

	// Prefer explicit Istio / OpenTelemetry service tags when present.
	if v := tagString(span, "istio.canonical_service"); v != "" {
		app = v
	}
	if v := tagString(span, "istio.namespace"); v != "" {
		ns = v
	}
	if v := tagString(span, "service.namespace"); v != "" {
		ns = v
	}
	if app == "" {
		if v := tagString(span, "peer.service"); v != "" {
			app, ns = splitServiceName(v, ns)
		}
	}
	return serviceIdentity{App: app, Namespace: ns}
}

func spanServiceName(trace jaegerModels.Trace, span *jaegerModels.Span) string {
	if span.Process != nil && span.Process.ServiceName != "" {
		return span.Process.ServiceName
	}
	if p, ok := trace.Processes[span.ProcessID]; ok {
		return p.ServiceName
	}
	return ""
}

// splitServiceName parses "app.namespace" (Istio convention) or bare "app".
func splitServiceName(svc, defaultNS string) (app, ns string) {
	svc = strings.TrimSpace(svc)
	if svc == "" {
		return "", defaultNS
	}
	// Strip FQDN suffixes like "productpage.bookinfo.svc.cluster.local"
	if i := strings.Index(svc, ".svc."); i > 0 {
		svc = svc[:i]
	}
	parts := strings.Split(svc, ".")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return parts[0], defaultNS
}

func tagString(span *jaegerModels.Span, key string) string {
	for _, t := range span.Tags {
		if t.Key == key && t.Value != nil {
			if s, ok := t.Value.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func ensureNode(tm graph.TrafficMap, opts BuildOptions, id serviceIdentity) *graph.Node {
	if id.App == "" {
		return nil
	}
	ns := id.Namespace
	if ns == "" {
		ns = opts.DefaultNamespace
	}

	service, workload, app, version := "", "", id.App, ""
	switch opts.GraphType {
	case graph.GraphTypeService:
		service = id.App
		app = ""
	case graph.GraphTypeWorkload, graph.GraphTypeVersionedApp:
		// Without workload tags, fall back to app-named workload so NewNode succeeds.
		workload = id.App
	}

	n, err := graph.NewNode(opts.Cluster, ns, service, ns, workload, app, version, opts.GraphType)
	if err != nil {
		return nil
	}
	if existing, ok := tm[n.ID]; ok {
		return existing
	}
	tm[n.ID] = n
	return n
}
