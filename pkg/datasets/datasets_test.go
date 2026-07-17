package datasets

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/parseablehq/pb/pkg/config"
)

func TestNamesByTypeUsesDatasetTypeMetadata(t *testing.T) {
	items := []Dataset{
		{Title: "custom-name", DatasetType: TypeLogs},
		{Title: "cpu", DatasetType: TypeMetrics},
		{Title: "logs-looking-trace", DatasetType: TypeTraces},
		{Title: "another-log", DatasetType: TypeLogs},
	}

	gotLogs := NamesByType(items, TypeLogs)
	wantLogs := []string{"another-log", "custom-name"}
	if !reflect.DeepEqual(gotLogs, wantLogs) {
		t.Fatalf("logs mismatch\nwant: %#v\n got: %#v", wantLogs, gotLogs)
	}

	gotMetrics := NamesByType(items, TypeMetrics)
	wantMetrics := []string{"cpu"}
	if !reflect.DeepEqual(gotMetrics, wantMetrics) {
		t.Fatalf("metrics mismatch\nwant: %#v\n got: %#v", wantMetrics, gotMetrics)
	}
}

func TestFetchAPIDatasetsUsesSinglePrismHomeRequest(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/api/prism/v1/home" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"datasets":[{"title":"traces","datasetType":"traces","datasetFormat":"otel-traces","ingestion":false},{"title":"logs","datasetType":"logs","datasetFormat":"otel-logs","ingestion":false}]}`)
	}))
	defer server.Close()

	items, err := FetchAPIDatasets(config.Profile{URL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 {
		t.Fatalf("request count=%d want=1", requestCount)
	}
	if len(items) != 2 || items[0].Title != "logs" || items[0].DatasetType != TypeLogs || items[1].DatasetType != TypeTraces {
		t.Fatalf("unexpected datasets: %#v", items)
	}
}
