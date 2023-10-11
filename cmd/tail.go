package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"pb/pkg/config"
	"strconv"
	"time"

	"github.com/apache/arrow/go/arrow"
	"github.com/apache/arrow/go/arrow/array"
	"github.com/apache/arrow/go/arrow/flight"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

var TailCmd = &cobra.Command{
	Use:     "tail stream-name",
	Example: " pb tail backend_logs",
	Short:   "tail a log stream",
	Args:    cobra.ExactArgs(1),
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		profile := DefaultProfile
		return tail(profile, name)
	},
}

func tail(profile config.Profile, stream string) error {
	payload, _ := json.Marshal(struct {
		Stream string `json:"stream"`
	}{
		Stream: stream,
	})
	client, err := flight.NewClientWithMiddleware(
		"localhost:8001",
		nil,
		nil,
		grpc.WithTransportCredentials(insecure.NewCredentials()))

	if err != nil {
		return err
	}

	authHeader := basicAuth(profile.Username, profile.Password)
	resp, err := client.DoGet(metadata.NewOutgoingContext(context.Background(), metadata.New(map[string]string{"Authorization": "Basic " + authHeader})), &flight.Ticket{
		Ticket: payload,
	})
	if err != nil {
		return err
	}

	records, err := flight.NewRecordReader(resp)
	if err != nil {
		return err
	}

	for true {
		if records.Next() {
			fmt.Println("here")
			record, err := records.Read()
			if err != nil {
				return err
			}
			recs := toPretty(*record.Schema(), record)
			fmt.Println(recs)
		}
	}

	defer records.Release()

	return nil
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func toPretty(schema arrow.Schema, record array.Record) [][]string {
	nullValue := "-"

	recs := make([][]string, record.NumRows())
	for i := range recs {
		recs[i] = make([]string, record.NumCols())
	}

	for j, col := range record.Columns() {
		ty := schema.Field(j).Type
		switch ty.(type) {
		case *arrow.BooleanType:
			arr := col.(*array.Boolean)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatBool(arr.Value(i))
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Int8Type:
			arr := col.(*array.Int8)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatInt(int64(arr.Value(i)), 10)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Int16Type:
			arr := col.(*array.Int16)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatInt(int64(arr.Value(i)), 10)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Int32Type:
			arr := col.(*array.Int32)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatInt(int64(arr.Value(i)), 10)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Int64Type:
			arr := col.(*array.Int64)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatInt(int64(arr.Value(i)), 10)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Uint8Type:
			arr := col.(*array.Uint8)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatUint(uint64(arr.Value(i)), 10)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Uint16Type:
			arr := col.(*array.Uint16)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatUint(uint64(arr.Value(i)), 10)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Uint32Type:
			arr := col.(*array.Uint32)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatUint(uint64(arr.Value(i)), 10)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Uint64Type:
			arr := col.(*array.Uint64)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatUint(uint64(arr.Value(i)), 10)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Float32Type:
			arr := col.(*array.Float32)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatFloat(float64(arr.Value(i)), 'g', -1, 32)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.Float64Type:
			arr := col.(*array.Float64)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = strconv.FormatFloat(float64(arr.Value(i)), 'g', -1, 64)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.StringType:
			arr := col.(*array.String)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = arr.Value(i)
				} else {
					recs[i][j] = nullValue
				}
			}
		case *arrow.TimestampType:
			arr := col.(*array.Time64)
			for i := 0; i < arr.Len(); i++ {
				if arr.IsValid(i) {
					recs[i][j] = time.UnixMilli(int64(arr.Value(i))).Format(time.RFC3339)
				} else {
					recs[i][j] = nullValue
				}
			}
		}
	}
	return recs
}
