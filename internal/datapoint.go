package internal

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	prom "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"
)

type dataPoint struct {
	ts    time.Time
	items map[string]*item
}

type itemKind int

const (
	kindCounter itemKind = iota
	kindGauge
	kindHistogramBucket
	kindHistogramSum
	kindHistogramCount
	kindSummarySum
	kindSummaryCount
)

// item represents a single metric item (e.g. a specific bucket) with its
// corresponding value.
type item struct {
	name  string
	kind  itemKind
	value float64
}

// newDataPoint parses the response returned from a Prometheus metrics endpoint
// (text format) and represents it as a dataPoint.
func newDataPoint(in io.Reader, ts time.Time) (*dataPoint, error) {
	dec := expfmt.NewDecoder(in, expfmt.FmtText)
	var mfs []*prom.MetricFamily
	for {
		mf := &prom.MetricFamily{}
		err := dec.Decode(mf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		mfs = append(mfs, mf)
	}
	items := flatten(mfs)
	dp := &dataPoint{
		ts:    ts,
		items: items,
	}
	return dp, nil
}

// flatten takes a map of Prometheus families and flattens them into an item-map.
func flatten(mfs []*prom.MetricFamily) map[string]*item {
	items := make(map[string]*item)

	// For each item family ...
	for _, mf := range mfs {
		mfName := mf.GetName()

		// For each item ...
		for _, m := range mf.GetMetric() {
			mLabels := m.GetLabel()
			mType := mf.GetType()
			switch mType {

			// A Histogram flattens into multiple "metrics" (each bucket, sum, and count).
			case prom.MetricType_HISTOGRAM, prom.MetricType_GAUGE_HISTOGRAM:
				for _, b := range m.GetHistogram().GetBucket() {
					roundedUpperBound := math.Round(b.GetUpperBound()*100) / 100
					roundedUpperBoundStr := strconv.FormatFloat(roundedUpperBound, 'f', -1, 64)
					bLabels := append(mLabels, &prom.LabelPair{
						Name:  proto.String("le"),
						Value: proto.String(roundedUpperBoundStr),
					})
					name := flatName(mfName+"_bucket", bLabels)
					value := b.GetCumulativeCountFloat()
					if value <= 0 {
						value = float64(b.GetCumulativeCount())
					}
					items[name] = &item{
						name:  name,
						kind:  kindHistogramBucket,
						value: value,
					}
				}
				name := flatName(mfName+"_sum", mLabels)
				items[name] = &item{
					name:  name,
					kind:  kindHistogramSum,
					value: m.GetHistogram().GetSampleSum(),
				}
				name = flatName(mfName+"_count", mLabels)
				value := m.GetHistogram().GetSampleCountFloat()
				if value <= 0 {
					value = float64(m.GetHistogram().GetSampleCount())
				}
				items[name] = &item{
					name:  name,
					kind:  kindHistogramCount,
					value: value,
				}

			// Counter.
			case prom.MetricType_COUNTER:
				name := flatName(mfName, mLabels)
				items[name] = &item{
					name:  name,
					kind:  kindCounter,
					value: m.GetCounter().GetValue(),
				}

			// Gauge.
			case prom.MetricType_GAUGE:
				name := flatName(mfName, mLabels)
				items[name] = &item{
					name:  name,
					kind:  kindGauge,
					value: m.GetGauge().GetValue(),
				}

			// Summary.
			case prom.MetricType_SUMMARY:
				name := flatName(mfName+"_sum", mLabels)
				items[name] = &item{
					name:  name,
					kind:  kindSummarySum,
					value: m.GetSummary().GetSampleSum(),
				}
				name = flatName(mfName+"_count", mLabels)
				items[name] = &item{
					name:  name,
					kind:  kindSummaryCount,
					value: float64(m.GetSummary().GetSampleCount()),
				}
			}
		}
	}
	return items
}

// flatName creates a flat name for the item and its labels.
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
