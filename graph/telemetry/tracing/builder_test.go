package tracing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kiali/kiali/config"
	"github.com/kiali/kiali/graph"
	jaegerModels "github.com/kiali/kiali/tracing/jaeger/model/json"
)

// bookinfoTrace builds a simplified Istio-style bookinfo request path:
//
//	productpage → reviews → ratings
//	productpage → details
func bookinfoTrace(traceID string) jaegerModels.Trace {
	return jaegerModels.Trace{
		TraceID: jaegerModels.TraceID(traceID),
		Processes: map[jaegerModels.ProcessID]jaegerModels.Process{
			"p1": {ServiceName: "productpage.bookinfo"},
			"p2": {ServiceName: "reviews.bookinfo"},
			"p3": {ServiceName: "ratings.bookinfo"},
			"p4": {ServiceName: "details.bookinfo"},
		},
		Spans: []jaegerModels.Span{
			{
				SpanID:        "s1",
				TraceID:       jaegerModels.TraceID(traceID),
				OperationName: "productpage",
				ProcessID:     "p1",
				Tags:          []jaegerModels.KeyValue{{Key: "span.kind", Type: jaegerModels.StringType, Value: "server"}},
			},
			{
				SpanID:        "s2",
				TraceID:       jaegerModels.TraceID(traceID),
				OperationName: "reviews",
				ProcessID:     "p2",
				References:    []jaegerModels.Reference{{RefType: jaegerModels.ChildOf, SpanID: "s1"}},
				Tags:          []jaegerModels.KeyValue{{Key: "span.kind", Type: jaegerModels.StringType, Value: "server"}},
			},
			{
				SpanID:        "s3",
				TraceID:       jaegerModels.TraceID(traceID),
				OperationName: "ratings",
				ProcessID:     "p3",
				References:    []jaegerModels.Reference{{RefType: jaegerModels.ChildOf, SpanID: "s2"}},
				Tags:          []jaegerModels.KeyValue{{Key: "span.kind", Type: jaegerModels.StringType, Value: "server"}},
			},
			{
				SpanID:        "s4",
				TraceID:       jaegerModels.TraceID(traceID),
				OperationName: "details",
				ProcessID:     "p4",
				References:    []jaegerModels.Reference{{RefType: jaegerModels.ChildOf, SpanID: "s1"}},
				Tags:          []jaegerModels.KeyValue{{Key: "span.kind", Type: jaegerModels.StringType, Value: "server"}},
			},
		},
	}
}

func TestBuildTrafficMapFromTraces_BookinfoTopology(t *testing.T) {
	tm := BuildTrafficMapFromTraces([]jaegerModels.Trace{bookinfoTrace("abc123")}, BuildOptions{})

	require.Len(t, tm, 4, "expected productpage, reviews, ratings, details")

	// Locate nodes by app
	byApp := map[string]*graph.Node{}
	for _, n := range tm {
		byApp[n.App] = n
		assert.Equal(t, "bookinfo", n.Namespace)
		assert.Equal(t, config.DefaultClusterID, n.Cluster)
		assert.Equal(t, graph.NodeTypeApp, n.NodeType)
	}

	require.Contains(t, byApp, "productpage")
	require.Contains(t, byApp, "reviews")
	require.Contains(t, byApp, "ratings")
	require.Contains(t, byApp, "details")

	// productpage → reviews, productpage → details
	ppEdges := destApps(byApp["productpage"])
	assert.ElementsMatch(t, []string{"reviews", "details"}, ppEdges)

	// reviews → ratings
	assert.ElementsMatch(t, []string{"ratings"}, destApps(byApp["reviews"]))

	// leaves have no outgoing edges
	assert.Empty(t, byApp["ratings"].Edges)
	assert.Empty(t, byApp["details"].Edges)

	// Call counts / protocol metadata
	for _, e := range byApp["productpage"].Edges {
		assert.Equal(t, graph.HTTP.Name, e.Metadata[graph.ProtocolKey])
		assert.Equal(t, float64(1), e.Metadata[TraceCallCount])
		assert.Equal(t, float64(1), e.Metadata[graph.MetadataKey(graph.HTTP.Name)])
		responses, ok := e.Metadata[graph.HTTP.EdgeResponses].(graph.Responses)
		require.True(t, ok, "edge must include httpResponses for config marshalling")
		assert.Contains(t, responses, "200")
		ids, ok := e.Metadata[TraceIDs].([]string)
		require.True(t, ok)
		assert.Equal(t, []string{"abc123"}, ids)
	}
}

func TestBuildTrafficMapFromTraces_AggregatesMultipleTraces(t *testing.T) {
	traces := []jaegerModels.Trace{
		bookinfoTrace("t1"),
		bookinfoTrace("t2"),
		bookinfoTrace("t3"),
	}
	tm := BuildTrafficMapFromTraces(traces, BuildOptions{})

	byApp := map[string]*graph.Node{}
	for _, n := range tm {
		byApp[n.App] = n
	}

	var reviewsEdge *graph.Edge
	for _, e := range byApp["productpage"].Edges {
		if e.Dest.App == "reviews" {
			reviewsEdge = e
			break
		}
	}
	require.NotNil(t, reviewsEdge)
	assert.Equal(t, float64(3), reviewsEdge.Metadata[TraceCallCount])

	ids, ok := reviewsEdge.Metadata[TraceIDs].([]string)
	require.True(t, ok)
	assert.Len(t, ids, 3)
}

func TestBuildTrafficMapFromTraces_SkipsIntraServiceSpans(t *testing.T) {
	// Client + server spans in the same service should not create a self-edge.
	trace := jaegerModels.Trace{
		TraceID: "intra",
		Processes: map[jaegerModels.ProcessID]jaegerModels.Process{
			"p1": {ServiceName: "reviews.bookinfo"},
		},
		Spans: []jaegerModels.Span{
			{
				SpanID:    "client",
				ProcessID: "p1",
				Tags:      []jaegerModels.KeyValue{{Key: "span.kind", Value: "client"}},
			},
			{
				SpanID:     "server",
				ProcessID:  "p1",
				References: []jaegerModels.Reference{{RefType: jaegerModels.ChildOf, SpanID: "client"}},
				Tags:       []jaegerModels.KeyValue{{Key: "span.kind", Value: "server"}},
			},
		},
	}

	tm := BuildTrafficMapFromTraces([]jaegerModels.Trace{trace}, BuildOptions{})
	require.Len(t, tm, 1)
	for _, n := range tm {
		assert.Equal(t, "reviews", n.App)
		assert.Empty(t, n.Edges, "intra-service spans must not create edges")
	}
}

func TestBuildTrafficMapFromTraces_UsesIstioTags(t *testing.T) {
	trace := jaegerModels.Trace{
		TraceID: "tags",
		Processes: map[jaegerModels.ProcessID]jaegerModels.Process{
			"p1": {ServiceName: "opaque-proxy"},
			"p2": {ServiceName: "opaque-proxy"},
		},
		Spans: []jaegerModels.Span{
			{
				SpanID:    "s1",
				ProcessID: "p1",
				Tags: []jaegerModels.KeyValue{
					{Key: "istio.canonical_service", Value: "productpage"},
					{Key: "istio.namespace", Value: "bookinfo"},
				},
			},
			{
				SpanID:     "s2",
				ProcessID:  "p2",
				References: []jaegerModels.Reference{{RefType: jaegerModels.ChildOf, SpanID: "s1"}},
				Tags: []jaegerModels.KeyValue{
					{Key: "istio.canonical_service", Value: "reviews"},
					{Key: "istio.namespace", Value: "bookinfo"},
				},
			},
		},
	}

	tm := BuildTrafficMapFromTraces([]jaegerModels.Trace{trace}, BuildOptions{})
	require.Len(t, tm, 2)

	byApp := map[string]*graph.Node{}
	for _, n := range tm {
		byApp[n.App] = n
		assert.Equal(t, "bookinfo", n.Namespace)
	}
	require.Contains(t, byApp, "productpage")
	require.Contains(t, byApp, "reviews")
	assert.ElementsMatch(t, []string{"reviews"}, destApps(byApp["productpage"]))
}

func TestBuildTrafficMapFromTraces_EmptyInput(t *testing.T) {
	tm := BuildTrafficMapFromTraces(nil, BuildOptions{})
	assert.Empty(t, tm)

	tm = BuildTrafficMapFromTraces([]jaegerModels.Trace{{TraceID: "empty"}}, BuildOptions{})
	assert.Empty(t, tm)
}

func TestSplitServiceName(t *testing.T) {
	cases := []struct {
		svc, defNS, wantApp, wantNS string
	}{
		{"productpage.bookinfo", "default", "productpage", "bookinfo"},
		{"productpage.bookinfo.svc.cluster.local", "default", "productpage", "bookinfo"},
		{"productpage", "bookinfo", "productpage", "bookinfo"},
		{"", "ns", "", "ns"},
	}
	for _, c := range cases {
		app, ns := splitServiceName(c.svc, c.defNS)
		assert.Equal(t, c.wantApp, app, "svc=%q", c.svc)
		assert.Equal(t, c.wantNS, ns, "svc=%q", c.svc)
	}
}

func destApps(n *graph.Node) []string {
	apps := make([]string, 0, len(n.Edges))
	for _, e := range n.Edges {
		apps = append(apps, e.Dest.App)
	}
	return apps
}
