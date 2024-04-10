package sinks

import (
	"context"
	"regexp"
	"strings"

	"k8s.io/utils/strings/slices"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/resmoio/kubernetes-event-exporter/pkg/kube"
	"github.com/rs/zerolog/log"
)

var (
	defaultMetricLabels []string = []string{"name", "namespace", "reason"}
	invalidCharsRegex            = regexp.MustCompile(`[^a-zA-Z0-9_]`)
	camelCaseRegex               = regexp.MustCompile("([a-z0-9])([A-Z])")
)

func newGaugeVec(opts prometheus.GaugeOpts, labelNames []string) *prometheus.GaugeVec {
	v := prometheus.NewGaugeVec(opts, labelNames)
	prometheus.MustRegister(v)
	return v
}

type PrometheusConfig struct {
	EventsMetricsNamePrefix string              `yaml:"eventsMetricsNamePrefix"`
	ReasonFilter            map[string][]string `yaml:"reasonFilter"`
	LabelFilter             map[string][]string `yaml:"labelFilter"`
}

type PrometheusGaugeVec interface {
	With(labels prometheus.Labels) prometheus.Gauge
	Delete(labels prometheus.Labels) bool
}

type PrometheusSink struct {
	cfg                *PrometheusConfig
	kinds              []string
	metricsByKind      map[string]PrometheusGaugeVec
	metricLabelsByKind map[string][]string
}

func NewPrometheusSink(config *PrometheusConfig) (Sink, error) {
	if config.EventsMetricsNamePrefix == "" {
		config.EventsMetricsNamePrefix = "event_exporter_"
	}

	metricsByKind := map[string]PrometheusGaugeVec{}
	metricLabelsByKind := map[string][]string{}

	log.Info().Msgf("Initializing new Prometheus sink...")
	kinds := []string{}
	for kind := range config.ReasonFilter {
		kinds = append(kinds, kind)
		labels := defaultMetricLabels
		if config.LabelFilter[kind] != nil {
			for _, label := range config.LabelFilter[kind] {
				if !slices.Contains(defaultMetricLabels, label) {
					labels = append(labels, label)
				}
			}
		}
		metricLabelsByKind[kind] = labels

		metricName := config.EventsMetricsNamePrefix + strings.ToLower(kind) + "_event_count"
		metricsByKind[kind] = newGaugeVec(
			prometheus.GaugeOpts{
				Name: metricName,
				Help: "Event counts for " + kind + " resources.",
			}, getMetricLabelNames(labels))

		log.Info().Msgf("Created metric: %s, will emit events: %v with additional labels: %v", kind, config.ReasonFilter[kind], labels)
	}

	return &PrometheusSink{
		cfg:                config,
		kinds:              kinds,
		metricsByKind:      metricsByKind,
		metricLabelsByKind: metricLabelsByKind,
	}, nil
}

func (o *PrometheusSink) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	kind := ev.InvolvedObject.Kind
	if slices.Contains(o.kinds, kind) {
		for _, reason := range o.cfg.ReasonFilter[kind] {
			if ev.Reason == reason {
				SetEventCount(o.metricsByKind[kind], o.metricLabelsByKind[kind], ev.InvolvedObject, reason, ev.Count)
			} else {
				DeleteEventCount(o.metricsByKind[kind], o.metricLabelsByKind[kind], ev.InvolvedObject, reason)
			}
		}
	}

	return nil
}

func (o *PrometheusSink) Close() {
	// No-op
}

func sanitizeLabelName(label string) string {
	// Uses kube-state-metrics label name format
	// See https://github.com/kubernetes/kube-state-metrics/blob/9ba1c3702142918e09e8eb5ca530e15198624259/internal/store/utils.go#L125
	label = invalidCharsRegex.ReplaceAllString(label, "_")
	label = camelCaseRegex.ReplaceAllString(label, "${1}_{2}")
	return strings.ToLower(label)
}

func getMetricLabelName(label string) string {
	if slices.Contains(defaultMetricLabels, label) {
		return label
	} else {
		labelName := "label_" + sanitizeLabelName(label)
		return labelName
	}
}

func getMetricLabelNames(labels []string) []string {
	labelNames := []string{}
	for _, label := range labels {
		labelNames = append(labelNames, getMetricLabelName(label))
	}

	return labelNames
}

func getMetricLabels(metricLabels []string, obj kube.EnhancedObjectReference, reason string) prometheus.Labels {
	prometheusLabels := prometheus.Labels{
		"name":      obj.Name,
		"namespace": obj.Namespace,
		"reason":    reason,
	}

	for _, label := range metricLabels {
		if !slices.Contains(defaultMetricLabels, label) {
			prometheusLabels[getMetricLabelName(label)] = obj.Labels[label]
		}
	}

	return prometheusLabels
}

func SetEventCount(metric PrometheusGaugeVec, metricLabels []string, obj kube.EnhancedObjectReference, reason string, count int32) {
	labels := getMetricLabels(metricLabels, obj, reason)
	log.Info().Msgf("Setting event count metric with labels: %v", labels)
	metric.With(labels).Set(float64(count))
}

func DeleteEventCount(metric PrometheusGaugeVec, metricLabels []string, obj kube.EnhancedObjectReference, reason string) {
	labels := getMetricLabels(metricLabels, obj, reason)
	log.Info().Msgf("Deleting event count metric with labels: %v", labels)
	metric.Delete(labels)
}
