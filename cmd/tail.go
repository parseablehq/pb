// Copyright (c) 2024 Parseable, Inc
//
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"pb/pkg/analytics"
	"pb/pkg/config"
	internalHTTP "pb/pkg/http"
	"time"

	"github.com/apache/arrow/go/v13/arrow/array"
	"github.com/apache/arrow/go/v13/arrow/flight"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var TailCmd = &cobra.Command{
	Use:     "tail dataset-name",
	Example: " pb tail backend_logs",
	Short:   "Stream live events from a dataset",
	Args:    cobra.ExactArgs(1),
	PreRunE: PreRunDefaultProfile,
	RunE: func(_ *cobra.Command, args []string) error {
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

	// get grpc url for this request
	httpClient := internalHTTP.DefaultClient(&DefaultProfile)
	about, err := analytics.FetchAbout(&httpClient)
	if err != nil {
		return err
	}
	url := profile.GrpcAddr(fmt.Sprint(about.GRPCPort))

	flightClient, err := flight.NewClientWithMiddleware(url, nil, nil, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}

	authHeader := basicAuth(profile.Username, profile.Password)

	for {
		resp, err := flightClient.DoGet(
			metadata.NewOutgoingContext(context.Background(), metadata.New(map[string]string{
				"Authorization": "Basic " + authHeader,
			})),
			&flight.Ticket{Ticket: payload},
		)
		if err != nil {
			return err
		}

		records, err := flight.NewRecordReader(resp)
		if err != nil {
			return err
		}

		for {
			record, err := records.Read()
			if err != nil {
				records.Release()
				if isStreamEnd(err) {
					break
				}
				return err
			}
			var buf bytes.Buffer
			array.RecordToJSON(record, &buf)
			fmt.Println(buf.String())
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// isStreamEnd returns true for normal stream termination codes that warrant a reconnect.
func isStreamEnd(err error) bool {
	if err == io.EOF {
		return true
	}
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Canceled, codes.Unavailable, codes.OK:
			return true
		}
	}
	return false
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
