package geoip

import (
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/caddytest"
)

type CustomTester struct {
	caddytest.Tester
	t *testing.T
}

func (tester *CustomTester) CheckResponse(requestURI string, testIP string, expectedStatusCode int,
	expectedHeader map[string]string, expectedBody string) *http.Response {
	req, _ := http.NewRequest("GET", requestURI, nil)
	req.Header.Add("X-Forwarded-For", testIP)
	resp, err := tester.Client.Do(req)
	if err != nil {
		tester.t.Fatalf("failed to call server %s", err)
	}
	if expectedStatusCode != resp.StatusCode {
		tester.t.Fatalf("requesting \"%s\" expected status code: %d but got %d",
			req.RequestURI, expectedStatusCode, resp.StatusCode)
	}

	defer resp.Body.Close()
	if expectedBody != "" {
		bytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			tester.t.Fatalf("unable to read the response body %s", err)
		}

		body := string(bytes)
		if !strings.Contains(body, expectedBody) {
			tester.t.Errorf("requesting \"%s\" expected response body \"%s\" but got \"%s\"",
				req.RequestURI, expectedBody, body)
		}
	}

	if expectedHeader != nil {
		for k, v := range expectedHeader {
			if resp.Header.Get(k) != v {
				tester.t.Errorf("fail to get geoip header %s, expected: %s but got %s", k, v, resp.Header.Get(k))
			}
		}
	}
	return resp
}

func TestAllowListResponse(t *testing.T) {
	tester := CustomTester{
		Tester: *caddytest.NewTester(t),
		t:      t,
	}

	// only US and 120.100.100.100 ip can visit
	tester.InitServer(`
	{
		order geoip first
	}

	:9000 {
		geoip cmd/GeoLite2-Country.mmdb {
			allow_list {
				country US
				ip 120.100.100.0
				allow_only true
			}
		}
		header Country-Code {geoip_country_code}
		header Country-Name {geoip_country_name}
		root * cmd/
		file_server
	}
  `, "caddyfile")

	// TW ip
	tester.CheckResponse("http://127.0.0.1:9000", "120.100.100.0", 200,
		map[string]string{"Country-Code": "TW", "Country-Name": "Taiwan"}, "Hi, test caddy geoip.")
	// US ip
	tester.CheckResponse("http://127.0.0.1:9000", "35.100.100.0", 200,
		map[string]string{"Country-Code": "US", "Country-Name": "United States"}, "Hi, test caddy geoip.")
	// SE ip
	tester.CheckResponse("http://127.0.0.1:9000", "212.100.100.0", 403, nil, "")

}

func TestBlockListResponse(t *testing.T) {
	tester := CustomTester{
		Tester: *caddytest.NewTester(t),
		t:      t,
	}

	// block TW and 35.100.100.0 IP, but 35.100.100.0 still can access due to the allow list.
	tester.InitServer(`
	{
		order geoip first
	}

	:9000 {
		geoip cmd/GeoLite2-Country.mmdb {
			block_list {
				country TW
				ip 35.100.100.0
			}
			allow_list {
				country US
				allow_only false
			}
		}
		header Country-Code {geoip_country_code}
		header Country-Name {geoip_country_name}
		root * cmd/
		file_server
	}
  `, "caddyfile")

	// TW ip
	tester.CheckResponse("http://127.0.0.1:9000", "120.100.100.0", 403, nil, "")
	// US ip
	tester.CheckResponse("http://127.0.0.1:9000", "35.100.100.0", 200,
		map[string]string{"Country-Code": "US", "Country-Name": "United States"}, "Hi, test caddy geoip.")
	// SE ip
	tester.CheckResponse("http://127.0.0.1:9000", "212.100.100.0", 200,
		map[string]string{"Country-Code": "SE", "Country-Name": "Sweden"}, "Hi, test caddy geoip.")
}
