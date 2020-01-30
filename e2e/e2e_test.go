package e2e

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/bzon/prometheus-msteams/pkg/card"
	"github.com/bzon/prometheus-msteams/pkg/service"
	"github.com/bzon/prometheus-msteams/pkg/testutils"
	"github.com/bzon/prometheus-msteams/pkg/transport"
	"github.com/go-kit/kit/log"
)

var update = flag.Bool("update", false, "update .golden files")

type alert struct {
	requestPath   string
	promAlertFile string
}

func TestServer(t *testing.T) {
	tmpl, err := card.ParseTemplateFile("../default-message-card.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	c := card.NewTemplatedCardCreator(tmpl)

	logger := log.NewJSONLogger(log.NewSyncWriter(os.Stderr))

	// Create a dummy Microsoft teams server.
	teamsSrv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := ioutil.ReadAll(r.Body)
			logger.Log("request", string(b))
			w.WriteHeader(200)
		}),
	)
	defer teamsSrv.Close()

	var testWebhookURL string
	// For Integration test.
	if v := os.Getenv("INTEGRATION_TEST_WEBHOOK_URL"); len(v) > 0 {
		t.Log("Running integration test")
		testWebhookURL = v
		// For Unit test.
	} else {
		testWebhookURL = teamsSrv.URL
	}

	tests := []struct {
		name   string
		routes []transport.Route
		alerts []alert
	}{
		{
			"templated card service test",
			[]transport.Route{
				transport.Route{
					RequestPath: "/alertmanager",
					Service: service.NewLoggingService(
						logger,
						service.NewSimpleService(
							c, http.DefaultClient, testWebhookURL,
						),
					),
				},
			},
			[]alert{
				alert{
					requestPath:   "/alertmanager",
					promAlertFile: "../pkg/card/testdata/prom_post_request.json",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the server and run it using a test http server.
			srv := transport.NewServer(logger, tt.routes...)
			testSrv := httptest.NewServer(srv)
			defer testSrv.Close()

			// Post the request for each alerts.
			for _, a := range tt.alerts {
				wm, err := testutils.ParseWebhookJSONFromFile(a.promAlertFile)
				if err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(wm)
				if err != nil {
					t.Fatal(err)
				}
				req, err := http.NewRequest(
					"POST",
					fmt.Sprintf("%s%s", testSrv.URL, a.requestPath),
					bytes.NewBuffer(b),
				)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("want '%d', got '%d'", 200, resp.StatusCode)
				}
				var prs []service.PostResponse
				if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
					t.Fatal(err)
				}
				testutils.CompareToGoldenFile(t, prs, t.Name()+"/resp.json", *update)
			}
		})
	}
}
