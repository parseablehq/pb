package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"pb/pkg/config"

	"github.com/apache/arrow/go/v13/arrow/array"
	"github.com/apache/arrow/go/v13/arrow/flight"
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
	client, err := flight.NewClientWithMiddleware("localhost:8001", nil, nil, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	defer records.Release()

	for {
		if records.Next() {
			record, err := records.Read()
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			array.RecordToJSON(record, &buf)
			fmt.Println(buf.String())
		}
	}
	return nil
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
