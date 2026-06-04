package datasets

import (
	"reflect"
	"testing"
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
