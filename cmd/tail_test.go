package cmd

import (
	"testing"

	"github.com/parseablehq/pb/pkg/config"
)

func TestTailTransportCredentials(t *testing.T) {
	tests := []struct {
		name         string
		profile      config.Profile
		wantProtocol string
		wantServer   string
	}{
		{
			name:         "cloud always uses TLS",
			profile:      config.Profile{URL: "https://workspace.example.com", Cloud: true},
			wantProtocol: "tls",
			wantServer:   "workspace.example.com",
		},
		{
			name:         "self-hosted HTTPS uses TLS",
			profile:      config.Profile{URL: "https://logs.example.com:8000"},
			wantProtocol: "tls",
			wantServer:   "logs.example.com",
		},
		{
			name:         "self-hosted HTTP preserves insecure transport",
			profile:      config.Profile{URL: "http://localhost:8000"},
			wantProtocol: "insecure",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transportCredentials, err := tailTransportCredentials(test.profile)
			if err != nil {
				t.Fatal(err)
			}
			info := transportCredentials.Info()
			if info.SecurityProtocol != test.wantProtocol {
				t.Fatalf("protocol=%q want=%q", info.SecurityProtocol, test.wantProtocol)
			}
			if info.ServerName != test.wantServer {
				t.Fatalf("server name=%q want=%q", info.ServerName, test.wantServer)
			}
		})
	}
}

func TestTailTransportCredentialsRejectsInvalidURL(t *testing.T) {
	if _, err := tailTransportCredentials(config.Profile{URL: "not-a-url"}); err == nil {
		t.Fatal("expected invalid URL error")
	}
}
