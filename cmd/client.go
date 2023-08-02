package cmd

import (
	"io"
	"net/http"
	"net/url"
	"pb/pkg/config"
	"time"
)

type HttpClient struct {
	client  http.Client
	profile *config.Profile
}

func DefaultClient() HttpClient {
	return HttpClient{
		client: http.Client{
			Timeout: 60 * time.Second,
		},
		profile: &DefaultProfile,
	}
}

func (client *HttpClient) baseApiUrl(path string) (x string) {
	x, _ = url.JoinPath(client.profile.Url, "api/v1/", path)
	return
}

func (client *HttpClient) NewRequest(method string, path string, body io.Reader) (req *http.Request, err error) {
	req, err = http.NewRequest(method, client.baseApiUrl(path), body)
	if err != nil {
		return
	}
	req.SetBasicAuth(client.profile.Username, client.profile.Password)
	req.Header.Add("Content-Type", "application/json")
	return
}
