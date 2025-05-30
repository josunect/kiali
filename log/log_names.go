package log

// these are variables used to define logger group names or other names of structured data fields used when logging messages
// see log.WithGroup for when these are typically used
var (
	GraphAppenderLogName = "appender"
	GraphLogName         = "graph"
	IstioConfigLogName   = "istioConfig"
	KialiCacheLogName    = "kialiCache"
	PromCacheLogName     = "promCache"
	PrometheusLogName    = "prometheus"
	TracingLogName       = "tracing"
	ValidationLogName    = "validation"
)
