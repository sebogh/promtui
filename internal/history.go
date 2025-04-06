package internal

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"

	prom "github.com/prometheus/client_model/go"
)

type History struct {
	endpoint string
	buf      *RingBuffer[Metrics]
}

func NewHistory(size int, endpoint string) *History {
	buf := NewRingBuffer[Metrics](size)
	return &History{
		endpoint: endpoint,
		buf:      buf,
	}
}

func (h *History) Add(m Metrics) {
	h.buf.Add(m)
}

func (h *History) Fetch() error {
	resp, err := http.Get(h.endpoint)
	if err != nil {
		return err
	}
	defer func() {
		errClose := resp.Body.Close()
		if errClose != nil {
			panic(fmt.Errorf("failed to close response body: %w", errClose))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("received status %d from metrics endpoint", resp.StatusCode)
	}

	ms, err := NewMetrics(resp.Body)
	if ms == nil {
		return fmt.Errorf("failed to parse metrics: %w", err)
	}
	h.buf.Add(ms)
	return nil
}

func (h *History) Len() int {
	return h.buf.Len()
}

func (h *History) Render(filter string) string {
	data := h.buf.Get()
	if len(data) == 0 {
		return "No Metrics"
	}
	current := data[len(data)-1]
	names := sortAndFilter(current, filter)
	if len(names) == 0 {
		return "No matching Metrics"
	}
	var sb strings.Builder
	for _, name := range names {
		vl := lineForMetric(data, name)
		sb.WriteString(vl.String() + "\n")
	}
	return sb.String()
}

type viewLine struct {
	name  string
	value string
	inc   bool
	dec   bool
}

func (vl viewLine) String() string {
	s := vl.name + " " + vl.value
	if vl.inc || vl.dec {
		s = colorBold + s + colorReset
	}
	if vl.inc {
		s += colorGreen + " ⬆" + colorReset
	}
	if vl.dec {
		s += colorRed + " ⬇" + colorReset
	}
	return s
}

func sortAndFilter(m Metrics, f string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if f == "" || strings.Contains(k, f) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func lineForMetric(data []Metrics, name string) viewLine {
	m := data[len(data)-1]
	cm, ok := m[name]

	if !ok {
		panic("metric not found")
	}
	p := m
	pm := cm
	if len(data) > 1 {
		p = data[len(data)-2]
		pm = p[name]
	}
	vl := viewLine{
		name: name,
	}
	switch cm.promType {
	case prom.MetricType_COUNTER:
		pcv := cm.metric.GetCounter().GetValue()
		vl.value = fmt.Sprintf("%d", uint64(pcv))
		if pm != nil {
			pv := pm.metric.GetCounter().GetValue()
			if pcv > pv {
				vl.inc = true
			} else if pcv < pv {
				vl.dec = true
			}
		}
	case prom.MetricType_GAUGE:
		gv := cm.metric.GetGauge().GetValue()
		vl.value = fmt.Sprintf("%d", uint64(gv))
		if pm != nil {
			pgv := pm.metric.GetGauge().GetValue()
			if gv > pgv {
				vl.inc = true
			} else if gv < pgv {
				vl.dec = true
			}
		}
	case prom.MetricType_HISTOGRAM:
		h := cm.metric.GetHistogram()
		avg := h.GetSampleSum() / float64(h.GetSampleCount())
		avg = math.Round(avg*100) / 100
		vl.value = fmt.Sprintf("avg: %.2f", avg)
		if pm != nil {
			ph := pm.metric.GetHistogram()
			pAvg := ph.GetSampleSum() / float64(ph.GetSampleCount())
			pAvg = math.Round(pAvg*100) / 100
			if avg > pAvg {
				vl.inc = true
			} else if avg < pAvg {
				vl.dec = true
			}
		}
	}
	return vl
}
