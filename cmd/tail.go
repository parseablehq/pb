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
	"os"
	"time"

	"github.com/apache/arrow/go/v13/arrow/array"
	"github.com/apache/arrow/go/v13/arrow/flight"
	"github.com/parseablehq/pb/pkg/analytics"
	"github.com/parseablehq/pb/pkg/config"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		output, err := cmd.Flags().GetString("output")
		if err != nil {
			return err
		}
		if output != "text" && output != "json" {
			return fmt.Errorf("unsupported output format %q (expected text or json)", output)
		}
		name := args[0]
		profile := DefaultProfile
		return tail(profile, name, output == "json")
	},
}

func init() {
	TailCmd.Flags().StringP("output", "o", "text", "Output format (text|json)")
}

func tail(profile config.Profile, stream string, jsonOutput bool) error {
	payload, _ := json.Marshal(struct {
		Stream string `json:"stream"`
	}{
		Stream: stream,
	})

	stopConnect := func() {}
	if !jsonOutput {
		stopConnect = tailSpinner("connecting...")
	}
	httpClient := internalHTTP.DefaultClient(&profile)
	about, err := analytics.FetchAbout(&httpClient)
	stopConnect()
	if err != nil {
		return err
	}
	url := profile.GrpcAddr(fmt.Sprint(about.GRPCPort))

	flightClient, err := flight.NewClientWithMiddleware(url, nil, nil, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}

	authMetadata, err := tailAuthMetadata(profile)
	if err != nil {
		return err
	}

	watching := func() {
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "\r\033[K● watching %s... (ctrl+c to stop)", stream)
		}
	}
	watching()

	for {
		resp, err := flightClient.DoGet(
			metadata.NewOutgoingContext(context.Background(), authMetadata),
			&flight.Ticket{Ticket: payload},
		)
		if err != nil {
			if !jsonOutput {
				fmt.Fprint(os.Stderr, "\r\033[K")
			}
			return err
		}

		records, err := flight.NewRecordReader(resp)
		if err != nil {
			if !jsonOutput {
				fmt.Fprint(os.Stderr, "\r\033[K")
			}
			return err
		}

		for {
			record, err := records.Read()
			if err != nil {
				records.Release()
				if isStreamEnd(err) {
					break
				}
				if !jsonOutput {
					fmt.Fprint(os.Stderr, "\r\033[K")
				}
				return err
			}
			if !jsonOutput {
				fmt.Fprint(os.Stderr, "\r\033[K")
			} // clear watching line before printing record
			var buf bytes.Buffer
			array.RecordToJSON(record, &buf)
			fmt.Println(buf.String())
		}

		watching()
		time.Sleep(500 * time.Millisecond)
	}
}

func tailAuthMetadata(profile config.Profile) (metadata.MD, error) {
	mode, err := profile.AuthMode()
	if err != nil {
		return nil, err
	}

	switch mode {
	case config.AuthCloudAPIKey:
		values := map[string]string{"x-api-key": profile.APIKey}
		values["x-p-tenant"] = profile.TenantID
		return metadata.New(values), nil
	case config.AuthCloudOAuth:
		values := map[string]string{"cookie": "session=" + profile.SessionToken}
		values["x-p-tenant"] = profile.TenantID
		return metadata.New(values), nil
	case config.AuthSelfHostedAPIKey:
		return metadata.New(map[string]string{
			"x-api-key": profile.APIKey,
		}), nil
	case config.AuthSelfHostedBasic:
		return metadata.New(map[string]string{
			"Authorization": "Basic " + basicAuth(profile.Username, profile.Password),
		}), nil
	default:
		return nil, fmt.Errorf("unsupported auth mode %q", mode)
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

func tailSpinner(msg string) func() {
	frames := []string{"|", "/", "-", "\\"}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		i := 0
		for {
			select {
			case <-done:
				fmt.Fprint(os.Stderr, "\r\033[K")
				return
			default:
				fmt.Fprintf(os.Stderr, "\r%s %s", frames[i%len(frames)], msg)
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
