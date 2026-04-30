package metrics

import (
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestPrometheusEndpoint(t *testing.T) {
	handler := promhttp.Handler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/metrics", nil)
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestMetricsRegistration(t *testing.T) {
	if RequestsTotal == nil {
		t.Fatal("RequestsTotal not registered")
	}
	if RequestDuration == nil {
		t.Fatal("RequestDuration not registered")
	}
	if InspectorDuration == nil {
		t.Fatal("InspectorDuration not registered")
	}
	if ActiveRequests == nil {
		t.Fatal("ActiveRequests not registered")
	}
	if RequestBodySize == nil {
		t.Fatal("RequestBodySize not registered")
	}
}

func TestRequestsTotal_Inc(t *testing.T) {
	RequestsTotal.WithLabelValues("ut_action", "ut_model").Inc()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "bulwarkai_requests_total" {
			for _, m := range mf.GetMetric() {
				if hasLabel(m.GetLabel(), "action", "ut_action") {
					if m.GetCounter().GetValue() < 1 {
						t.Fatal("expected counter >= 1")
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("metric not found")
	}
}

func TestActiveRequests_IncDec(t *testing.T) {
	ActiveRequests.Inc()
	ActiveRequests.Dec()
}

func TestRequestBodySize_Observe(t *testing.T) {
	RequestBodySize.Observe(1024)
}

func TestInspectorDuration_Observe(t *testing.T) {
	InspectorDuration.WithLabelValues("regex", "prompt").Observe(0.001)
}

func TestRequestDuration_Observe(t *testing.T) {
	RequestDuration.WithLabelValues("request").Observe(0.1)
}

func hasLabel(lps []*dto.LabelPair, name, value string) bool {
	for _, lp := range lps {
		if lp.GetName() == name && lp.GetValue() == value {
			return true
		}
	}
	return false
}

func TestInspectorResultsCounter(t *testing.T) {
	InspectorResults.WithLabelValues("regex", "prompt", "pass").Inc()
	count := testutil.ToFloat64(InspectorResults.WithLabelValues("regex", "prompt", "pass"))
	if count == 0 {
		t.Fatal("expected counter to be registered")
	}
}

func TestPolicyResultsCounter(t *testing.T) {
	PolicyResults.WithLabelValues("allow").Inc()
	count := testutil.ToFloat64(PolicyResults.WithLabelValues("allow"))
	if count == 0 {
		t.Fatal("expected counter to be registered")
	}
}
