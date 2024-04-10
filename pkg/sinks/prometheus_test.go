package sinks

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/resmoio/kubernetes-event-exporter/pkg/kube"
	"github.com/stretchr/testify/mock"
)

type mockGauge struct {
	mock.Mock
	prometheus.Gauge
}

func (m *mockGauge) Set(count float64) {
	m.Called(count)
}

type mockGuageVec struct {
	mock.Mock
	*prometheus.GaugeVec
}

func (v *mockGuageVec) With(labels prometheus.Labels) prometheus.Gauge {
	withArgs := v.Called(labels)
	return withArgs.Get(0).(prometheus.Gauge)
}

func (v *mockGuageVec) Delete(labels prometheus.Labels) bool {
	deleteArgs := v.Called(labels)
	return deleteArgs.Get(0).(bool)
}

func mockEvent(kind string, name string, namespace string, labels map[string]string, reason string, count int32) *kube.EnhancedEvent {
	ev := &kube.EnhancedEvent{}
	ev.Reason = reason
	ev.Count = count
	ev.InvolvedObject.Kind = kind
	ev.InvolvedObject.Name = name
	ev.InvolvedObject.Namespace = namespace
	ev.InvolvedObject.Labels = labels

	return ev
}

func TestPrometheusSink_Send(t *testing.T) {
	configKind := "Pod"
	configReason := "Starting"
	configLabel := "test_label"
	testEvent := mockEvent("Pod", "testpod", "testnamespace", map[string]string{configLabel: "test_label_value"}, "Starting", 1)

	tests := []struct {
		name                  string
		configKind            string
		configReason          string
		configAdditionalLabel string
		ev                    *kube.EnhancedEvent
		wantPrometheusLabels  prometheus.Labels
		wantErr               bool
		wantSetCalled         bool
		wantDeleteCalled      bool
	}{
		{
			name:                  "emits desired resource event with resource label",
			configKind:            configKind,
			configReason:          configReason,
			configAdditionalLabel: configLabel,
			ev:                    testEvent,
			wantPrometheusLabels: prometheus.Labels{
				"name":                  testEvent.InvolvedObject.Name,
				"namespace":             testEvent.InvolvedObject.Namespace,
				"reason":                configReason,
				"label_" + configLabel: testEvent.InvolvedObject.Labels[configLabel],
			},
			wantErr:          false,
			wantSetCalled:    true,
			wantDeleteCalled: false,
		},
		{
			name:                  "deletes desired resource event with resource label",
			configKind:            configKind,
			configReason:          "Creating",
			configAdditionalLabel: configLabel,
			ev:                    testEvent,
			wantPrometheusLabels: prometheus.Labels{
				"name":                  testEvent.InvolvedObject.Name,
				"namespace":             testEvent.InvolvedObject.Namespace,
				"reason":                "Creating",
				"label_" + configLabel: testEvent.InvolvedObject.Labels[configLabel],
			},
			wantErr:          false,
			wantSetCalled:    false,
			wantDeleteCalled: true,
		},
		{
			name:                  "does nothing if kind is not expected",
			configKind:            "ReplicaSet",
			configReason:          "SuccessfulCreate",
			configAdditionalLabel: configLabel,
			ev:                    testEvent,
			wantPrometheusLabels:  prometheus.Labels{},
			wantErr:               false,
			wantSetCalled:         false,
			wantDeleteCalled:      false,
		},
	}
	for _, tt := range tests {
		mockGauge := &mockGauge{}
		mockGauge.On("Set", mock.Anything).Return()
		mockPodMetric := &mockGuageVec{}
		mockPodMetric.On("With", mock.Anything).Return(mockGauge)
		mockPodMetric.On("Delete", mock.Anything).Return(true)
		additionalLabels := []string{tt.configAdditionalLabel}

		t.Run(tt.name, func(t *testing.T) {
			o := &PrometheusSink{
				cfg: &PrometheusConfig{
					EventsMetricsNamePrefix: "test_prefix_",
					ReasonFilter:            map[string][]string{tt.configKind: {tt.configReason}},
					LabelFilter:             map[string][]string{tt.configKind: additionalLabels},
				},
				kinds:              []string{tt.configKind},
				metricsByKind:      map[string]PrometheusGaugeVec{tt.configKind: mockPodMetric},
				metricLabelsByKind: map[string][]string{tt.configKind: append(defaultMetricLabels, additionalLabels...)},
			}
			if err := o.Send(context.TODO(), tt.ev); (err != nil) != tt.wantErr {
				t.Errorf("PrometheusSink.Send() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantSetCalled {
				mockPodMetric.AssertCalled(t, "With", tt.wantPrometheusLabels)
				mockGauge.AssertCalled(t, "Set", float64(1))
			} else {
				mockPodMetric.AssertNotCalled(t, "With")
				mockGauge.AssertNotCalled(t, "Set")
			}

			if tt.wantDeleteCalled {
				mockPodMetric.AssertCalled(t, "Delete", tt.wantPrometheusLabels)
			} else {
				mockPodMetric.AssertNotCalled(t, "Delete")
			}
		})
	}
}
