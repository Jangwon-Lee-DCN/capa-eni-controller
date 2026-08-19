package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	PoolAvailableInterfaces = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "capa_eni_pool_available_interfaces",
		Help: "Number of currently available ENIs in an ENIPool.",
	}, []string{"pool", "region", "vpc_id"})

	DynamicFallbacks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "capa_eni_dynamic_fallbacks_total",
		Help: "Number of AWSMachines released to CAPA dynamic networking.",
	}, []string{"reason"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(PoolAvailableInterfaces, DynamicFallbacks)
}
