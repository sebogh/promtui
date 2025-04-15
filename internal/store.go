package internal

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maruel/natural"
	prom "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"
)

const (
	ObservationCounter ObservationKind = iota
	ObservationCounterRate
	ObservationGauge
	ObservationHistogramBucket
	ObservationHistogramSum
	ObservationHistogramCount
	ObservationHistogramAvg
	ObservationSummarySum
	ObservationSummaryCount
)

var promFormat = expfmt.NewFormat(expfmt.TypeTextPlain)

// Store is a structure that holds observations of different metrics over time.
type Store struct {
	endpoint string
	rb       *ringBuffer[map[string]Observation]
	mux      sync.RWMutex
}

// Observation represents a single observation (e.g. the value of a given metric
// at a given time).
type Observation struct {

	// Name is the name of the metric.
	Name string

	// Kind is the type of observation (e.g. counter, gauge, etc.).
	Kind ObservationKind

	// Time is the time of the observation (when the observation was fetched).
	Time time.Time

	// Value is the value of the observation.
	Value float64
}

// ObservationKind represents the type of observation (e.g. counter, gauge, etc.).
type ObservationKind int

// NewStore returns a new Store.
func NewStore(size int, endpoint string) *Store {
	return &Store{
		endpoint: endpoint,
		rb:       newRingBuffer[map[string]Observation](size),
	}
}

// NewObservation creates a new Observation.
func NewObservation(name string, kind ObservationKind, ts time.Time, value float64) Observation {
	return Observation{
		Name:  name,
		Kind:  kind,
		Time:  ts,
		Value: value,
	}
}

// Sample fetches a set of observations (metrics) from the endpoint and adds it
// to them to the store. Sample returns:
//   - true and nil, if new observations were fetched and added to the store,
//   - false and nil, if no new observations were fetched nor added (because of
//     a concurrent Sample-call), and
//   - false and an error, if something went wrong while fetching.
func (h *Store) Sample() (bool, error) {
	if !h.mux.TryLock() {
		return false, nil
	}
	defer h.mux.Unlock()

	req, err := http.NewRequest(http.MethodGet, h.endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", string(promFormat))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, fmt.Errorf("unexpected status")
	}

	obs, err := newObservationSet(resp.Body)
	if err != nil {
		return false, fmt.Errorf("parse response: %w", err)
	}
	h.rb.add(obs)
	return true, nil
}

// Dump dumps the store. Dump returns a sorted list of different metrics and their
// observations over time. If a non-empty filter is given, only the metrics
// matching the filter are returned.
func (h *Store) Dump(f string) ([][]Observation, error) {
	h.mux.RLock()
	data := h.rb.get()
	h.mux.RUnlock()

	if len(data) == 0 {
		return nil, fmt.Errorf("no data points")
	}

	names := filterAndSort(data[len(data)-1], f)
	var dump [][]Observation
	for _, name := range names {
		values := getSeries(data, name)
		if len(values) == 0 {
			continue
		}
		dump = append(dump, values)
	}
	return dump, nil
}

// filterAndSort returns a filtered and sorted list of metric names from the
// given set of observations.
func filterAndSort(obs map[string]Observation, f string) []string {
	names := make([]string, 0, len(obs))
	for k := range obs {
		if f == "" || strings.Contains(strings.ToLower(k), strings.ToLower(f)) {
			names = append(names, k)
		}
	}
	sort.Sort(natural.StringSlice(names))
	return names
}

// getSeries returns the series of observations for a given metric-name over
// time. Observations are sorted from youngest to oldest. getSeries returns an
// empty slice, if the latest timestamp does not contain an observation for the
// given metric-name. If any of the previous timestamps does not contain an
// observation for the given metric-name, getSeries returns the values up to that
// point.
func getSeries(data []map[string]Observation, name string) []Observation {
	series := make([]Observation, 0, len(data))
	for i := len(data) - 1; i >= 0; i-- {
		o, ok := data[i][name]
		if !ok {
			return series
		}
		series = append(series, o)
	}
	return series
}

// newObservationSet parses the response returned from a Prometheus metrics endpoint
// and returns a set (map) of observations.
func newObservationSet(in io.Reader) (map[string]Observation, error) {
	ts := time.Now()
	dec := expfmt.NewDecoder(in, promFormat)
	var mfs []*prom.MetricFamily

	for {
		mf := &prom.MetricFamily{}
		if err := dec.Decode(mf); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		mfs = append(mfs, mf)
	}
	return flatten(mfs, ts), nil
}

// flatten takes a map of Prometheus families and flattens them into a map of observations.
func flatten(mfs []*prom.MetricFamily, ts time.Time) map[string]Observation {
	obs := make(map[string]Observation, len(mfs))

	for _, mf := range mfs {
		mfName := mf.GetName()

		for _, m := range mf.GetMetric() {
			mLabels := m.GetLabel()
			mType := mf.GetType()
			switch mType {

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
					obs[name] = NewObservation(name, ObservationHistogramBucket, ts, value)
				}

				name := flatName(mfName+"_sum", mLabels)
				sampleSum := m.GetHistogram().GetSampleSum()
				obs[name] = NewObservation(name, ObservationHistogramSum, ts, sampleSum)

				name = flatName(mfName+"_count", mLabels)
				sampleCount := m.GetHistogram().GetSampleCountFloat()
				if sampleCount <= 0 {
					sampleCount = float64(m.GetHistogram().GetSampleCount())
				}
				obs[name] = NewObservation(name, ObservationHistogramCount, ts, sampleCount)

				if sampleCount > 0 {
					avg := sampleSum / sampleCount
					name = flatName(mfName+"_avg", mLabels)
					obs[name] = NewObservation(name, ObservationHistogramAvg, ts, avg)
				}

			case prom.MetricType_COUNTER:
				name := flatName(mfName, mLabels)
				obs[name] = NewObservation(name, ObservationCounter, ts, m.GetCounter().GetValue())

			case prom.MetricType_GAUGE:
				name := flatName(mfName, mLabels)
				obs[name] = NewObservation(name, ObservationGauge, ts, m.GetGauge().GetValue())

			case prom.MetricType_SUMMARY:
				name := flatName(mfName+"_sum", mLabels)
				obs[name] = NewObservation(name, ObservationSummarySum, ts, m.GetSummary().GetSampleSum())

				name = flatName(mfName+"_count", mLabels)
				obs[name] = NewObservation(name, ObservationSummaryCount, ts, float64(m.GetSummary().GetSampleCount()))
			}
		}
	}
	return obs
}

// flatName creates a flat Name for the Observation and its labels.
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
