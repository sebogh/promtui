package internal

import (
	"fmt"
	"io"
	"strings"

	prom "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

const (
	colorBold  = "\033[1m"
	colorRed   = "\033[31m"
	colorGreen = "\033[32m"
	colorReset = "\033[0m"
)

type Metrics map[string]*metric

type metric struct {
	name     string
	promType prom.MetricType
	metric   *prom.Metric
}

func NewMetrics(in io.Reader) (Metrics, error) {
	var parser expfmt.TextParser
	mf, err := parser.TextToMetricFamilies(in)
	if err != nil {
		return nil, err
	}
	return flatten(mf), nil
}

func flatten(m map[string]*prom.MetricFamily) map[string]*metric {
	flat := make(map[string]*metric)
	for _, family := range m {
		for _, met := range family.GetMetric() {
			labels := met.GetLabel()
			name := flatName(family.GetName(), labels)
			flat[name] = &metric{
				name:     name,
				promType: family.GetType(),
				metric:   met,
			}
		}
	}
	return flat
}

func flatName(name string, labels []*prom.LabelPair) string {
	if len(labels) == 0 {
		return name
	}
	labelParts := make([]string, 0, len(labels))
	for _, label := range labels {
		labelParts = append(labelParts, fmt.Sprintf("%s=%q", label.GetName(), label.GetValue()))
	}
	return name + " {" + strings.Join(labelParts, ", ") + "}"
}
