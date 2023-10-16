// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
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
		record, err := records.Read()
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		array.RecordToJSON(record, &buf)
		fmt.Println(buf.String())
	}
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
