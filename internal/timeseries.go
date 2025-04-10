package internal

import (
	"fmt"
	"github.com/maruel/natural"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// TimeSeries is a structure that holds a ring buffer of metrics and an endpoint
// to fetch them from.
type TimeSeries struct {
	endpoint string
	buf      *RingBuffer[dataPoint]
	mux      sync.RWMutex
}

// ItemSeries is used to represent a single metric-item (e.g. a specific bucket)
// across multiple data points. ItemSeries contains the item name and
// corresponding values (from youngest to oldest). TimeSeries.Dump returns a
// slice of ItemSeries.
type ItemSeries struct {
	Name   string
	Values []float64
}

// NewTimeSeries creates a new TimeSeries with the given size and endpoint.
func NewTimeSeries(size int, endpoint string) *TimeSeries {
	buf := NewRingBuffer[dataPoint](size)
	return &TimeSeries{
		endpoint: endpoint,
		buf:      buf,
	}
}

// Sample fetches metrics from the endpoint and adds them to the TimeSeries.
// Sample returns true and no error, if new metrics were fetched and added to the
// TimeSeries. If there is a concurrent sample in progress, it returns false and
// no error. If there is an error while fetching the metrics, it returns false
// and the error.
func (h *TimeSeries) Sample() (bool, error) {
	if !h.mux.TryLock() {
		return false, nil
	}
	defer h.mux.Unlock()

	resp, err := http.Get(h.endpoint)
	if err != nil {
		return false, err
	}
	defer func() {
		errClose := resp.Body.Close()
		if errClose != nil {
			panic(fmt.Errorf("failed to close response body: %w", errClose))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, fmt.Errorf("received status %d from metrics endpoint", resp.StatusCode)
	}
	dp, err := newDataPoint(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to parse metrics: %w", err)
	}
	h.buf.Add(dp)
	return true, nil
}

// Dump returns (dumps) ItemSeries from the TimeSeries. It filters the items by
// name based on the provided filter string. If the filter string is empty, all
// metrics are included.
func (h *TimeSeries) Dump(filter string) ([]ItemSeries, error) {
	h.mux.RLock()
	dps := h.buf.Get()
	h.mux.RUnlock()
	if len(dps) == 0 {
		return nil, fmt.Errorf("no data points")
	}
	current := dps[len(dps)-1]
	names := sortAndFilter(current, filter)
	result := make([]ItemSeries, 0)
	for _, name := range names {
		values := itemValues(dps, name)
		if len(values) == 0 {
			continue
		}
		is := ItemSeries{
			Name:   name,
			Values: values,
		}
		result = append(result, is)
	}
	return result, nil
}

// sortAndFilter sorts the keys of a dataPoint and filters them based on the
// provided filter string. If the filter string is empty, all keys are included.
func sortAndFilter(m dataPoint, f string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if f == "" || strings.Contains(strings.ToLower(k), strings.ToLower(f)) {
			keys = append(keys, k)
		}
	}
	sort.Sort(natural.StringSlice(keys))
	return keys
}

// itemValues returns the values of a specific item across from youngest to
// oldest. If the latest value is not found, it returns an empty slice. If any of
// the previous values are not found, it returns the values up to that point.
func itemValues(dps []dataPoint, name string) []float64 {
	values := make([]float64, 0, len(dps))
	for j := len(dps) - 1; j >= 0; j-- {
		i, ok := dps[j][name]
		if !ok {
			return values
		}
		values = append(values, i.value)
	}
	return values
}
