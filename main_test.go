package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-playground/assert/v2"
)

func TestGetConfig(t *testing.T) {
	cases := []struct {
		name           string
		args           []string
		expectedFail   bool
		expectedConfig Endpoints
	}{
		{
			name:         "No Arguments Provided",
			args:         []string{"CheckHealth"},
			expectedFail: true,
		},
		{
			name:         "Too Many Arguments Provided",
			args:         []string{"CheckHealth", "config.yaml", "foo"},
			expectedFail: true,
		},
		{
			name:         "File Does Not Exist",
			args:         []string{"CheckHealth", "file.txt"},
			expectedFail: true,
		},
		{
			name:         "File Has Invalid Format",
			args:         []string{"CheckHealth", "README.md"},
			expectedFail: true,
		},
		{
			name:         "General Case",
			args:         []string{"CheckHealth", "config.yaml"},
			expectedFail: false,
			expectedConfig: Endpoints{
				{
					Name:    "fetch.com index page",
					Url:     "https://fetch.com/",
					Method:  "GET",
					Headers: map[string]string{"user-agent": "fetch-synthetic-monitor"},
				},
				{
					Name:    "fetch.com careers page",
					Url:     "https://fetch.com/careers",
					Method:  "GET",
					Headers: map[string]string{"user-agent": "fetch-synthetic-monitor"},
				},
				{
					Name:   "fetch.com some post endpoint",
					Url:    "https://fetch.com/some/post/endpoint",
					Method: "POST",
					Headers: map[string]string{
						"content-type": "application/json",
						"user-agent":   "fetch-synthetic-monitor",
					},
					Body: `{"foo":"bar"}`,
				},
				{
					Name: "www.fetchrewards.com index page",
					Url:  "https://www.fetchrewards.com/",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// save off os.Args & replace with tc.args
			actualArgs := os.Args
			os.Args = tc.args

			// run GetConfig()
			config, err := GetConfig()
			if tc.expectedFail {
				assert.NotEqual(t, err, nil)
				os.Args = actualArgs
				return
			}

			// validate expected output
			assert.Equal(t, config, tc.expectedConfig)

			// swap os.Args back in place
			os.Args = actualArgs
		})
	}
}

func TestCreateRequest(t *testing.T) {
	cases := []struct {
		name           string
		endpoint       Endpoint
		expectedError  error
		expectedHeader http.Header
	}{
		{
			name: "GET request with no body or headers",
			endpoint: Endpoint{
				Url:     "http://example.com/",
				Method:  "GET",
				Body:    "",
				Headers: nil,
			},
			expectedError:  nil,
			expectedHeader: http.Header{},
		},
		{
			name: "POST request with body and headers",
			endpoint: Endpoint{
				Url:    "https://fetch.com/some/post/endpoint",
				Method: "POST",
				Body:   `{"foo":"bar"}`,
				Headers: map[string]string{
					"content-type": "application/json",
					"user-agent":   "fetch-synthetic-monitor",
				},
			},
			expectedError: nil,
			expectedHeader: http.Header{
				"Content-Type": {"application/json"},
				"User-Agent":   {"fetch-synthetic-monitor"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, err := tc.endpoint.CreateRequest(context.Background())

			if tc.expectedError != nil {
				assert.Equal(t, err, tc.expectedError)
				return
			}

			assert.Equal(t, err, nil)

			// confirm request methods is the input method
			assert.Equal(t, request.Method, tc.endpoint.Method)

			// confirm that requested URL is correct
			expectedURL, err := url.Parse(tc.endpoint.Url)
			assert.Equal(t, err, nil)
			assert.Equal(t, *request.URL, *expectedURL)

			// confirm body populates request
			if tc.endpoint.Body != "" {
				var requestBody []byte
				requestBody, err := io.ReadAll(request.Body)
				assert.Equal(t, err, nil)
				assert.Equal(t, requestBody, []byte(tc.endpoint.Body))
			} else {
				assert.Equal(t, request.Body, nil)
			}

			// confirm headers populate correctly
			assert.Equal(t, request.Header, tc.expectedHeader)
		})
	}
}

func TestCreateNewTargets(t *testing.T) {
	tc := struct {
		name                   string
		config                 Endpoints
		expectedTargetContents HealthCheckTargets
		expectedDomains        []string
	}{
		name: "Multiple Endpoints in Config",
		config: Endpoints{
			{
				Name:   "fetch.com some post endpoint",
				Url:    "https://fetch.com/some/post/endpoint",
				Method: "POST",
				Headers: map[string]string{
					"content-type": "application/json",
					"user-agent":   "fetch-synthetic-monitor",
				},
				Body: `{"foo":"bar"}`,
			},
			{
				Name: "www.fetchrewards.com index page",
				Url:  "https://www.fetchrewards.com/",
			},
		},
		expectedDomains: []string{"fetch.com", "www.fetchrewards.com"},
	}

	// create new target using provided endpoint config
	targets, err := tc.config.CreateNewTargets()
	if err != nil {
		t.Errorf("CreateNewTargets failed. Wants: nil, Got: %v", err)
		return
	}

	// cycle through Domains to validate content
	current_domain := targets.Domains
	for i := 0; i < len(tc.expectedDomains); i++ {
		// verify domains look correct
		assert.Equal(t, current_domain.Name, tc.expectedDomains[i])
		assert.Equal(t, current_domain.UpCount, 0)
		assert.Equal(t, current_domain.TotalRequests, 0)

		if i == (len(tc.expectedDomains) - 1) {
			assert.Equal(t, current_domain.Next, nil)
		}

		current_domain = current_domain.Next

		// verify endpoints point to the correct domain
		assert.Equal(t, (*targets.Endpoints)[i].Domain.Name, tc.expectedDomains[i])
	}
}

func TestGetDomainPointer(t *testing.T) {
	cases := []struct {
		name                   string
		target                 *HealthCheckTargets
		url                    string
		expectedFail           bool
		expectedDomainName     string
		expectedDomainsContent *Domain
	}{
		{
			name: "No Domains Exists",
			target: &HealthCheckTargets{
				Domains: nil,
			},
			url:                "http://example.com/",
			expectedFail:       false,
			expectedDomainName: "example.com",
			expectedDomainsContent: &Domain{
				Name:          "example.com",
				UpCount:       0,
				TotalRequests: 0,
				Next:          nil,
			},
		},
		{
			name: "Domain List Contains Domain Name",
			target: &HealthCheckTargets{
				Domains: &Domain{
					Name:          "example.com",
					UpCount:       1,
					TotalRequests: 1,
					Next:          nil,
				},
			},
			url:                "http://example.com/",
			expectedFail:       false,
			expectedDomainName: "example.com",
			expectedDomainsContent: &Domain{
				Name:          "example.com",
				UpCount:       1,
				TotalRequests: 1,
				Next:          nil,
			},
		},
		{
			name: "Domain List Exist and Does Not Contain Domain Name",
			target: &HealthCheckTargets{
				Domains: &Domain{
					Name:          "fetch.com",
					UpCount:       0,
					TotalRequests: 0,
					Next:          nil,
				},
			},
			url:                "http://example.com/",
			expectedFail:       false,
			expectedDomainName: "example.com",
			expectedDomainsContent: &Domain{
				Name:          "fetch.com",
				UpCount:       0,
				TotalRequests: 0,
				Next: &Domain{
					Name:          "example.com",
					UpCount:       0,
					TotalRequests: 0,
					Next:          nil,
				},
			},
		},
		{
			name: "Provided URL is Blank",
			target: &HealthCheckTargets{
				Domains: nil,
			},
			url:          "",
			expectedFail: true,
		},
		{
			name:         "Provided Target is nil",
			target:       nil,
			url:          "http://example.com/",
			expectedFail: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			domain_pointer, err := tc.target.GetDomainPointer(tc.url)

			// handle if we expect to fail
			if tc.expectedFail {
				assert.NotEqual(t, err, nil)
				return
			}

			// validate previous domains remain available and untouched
			// verify that the new domain exists and has correct values
			current_domain := tc.target.Domains
			reference_domain := tc.expectedDomainsContent
			for current_domain != nil {
				assert.Equal(t, current_domain.Name, reference_domain.Name)
				assert.Equal(t, current_domain.UpCount, reference_domain.UpCount)
				assert.Equal(t, current_domain.TotalRequests, reference_domain.TotalRequests)

				// current domain and next domain should be the same size,
				if current_domain.Next == nil {
					assert.Equal(t, reference_domain.Next, nil)
				}

				current_domain = current_domain.Next
				reference_domain = reference_domain.Next
			}

			// verify the returned pointer matches a pointer in the domain list
			assert.Equal(t, domain_pointer.Name, tc.expectedDomainName)
		})
	}
}

func TestUpdateDomainStats(t *testing.T) {
	cases := []struct {
		name          string
		domain        *Domain
		inputStatus   bool
		isNil         bool
		expectedUp    int
		expectedTotal int
	}{
		{
			name:        "Domain Pointer is Nil",
			domain:      nil,
			inputStatus: EndpointDown,
			isNil:       true,
		},
		{
			name: "Endpoint Is Up",
			domain: &Domain{
				Name:          "example.com",
				UpCount:       0,
				TotalRequests: 0,
				Next:          nil,
			},
			inputStatus:   EndpointUp,
			isNil:         false,
			expectedUp:    1,
			expectedTotal: 1,
		},
		{
			name: "Endpoint Is Down",
			domain: &Domain{
				Name:          "example.com",
				UpCount:       0,
				TotalRequests: 0,
				Next:          nil,
			},
			inputStatus:   EndpointDown,
			isNil:         false,
			expectedUp:    0,
			expectedTotal: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.domain.UpdateDomainStats(tc.inputStatus)

			if tc.isNil {
				assert.Equal(t, tc.domain, nil)
				return
			}

			assert.Equal(t, tc.domain.UpCount, tc.expectedUp)
			assert.Equal(t, tc.domain.TotalRequests, tc.expectedTotal)
		})
	}
}

func TestGetEndpointHealth(t *testing.T) {
	var delay bool = false

	mock_server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, r.Method, "POST")
		assert.Equal(t, r.Header, http.Header{
			// header values expected as part of this mock request
			"Accept-Encoding": []string{"gzip"},
			"Content-Length":  []string{"13"},

			// values we expect
			"Content-Type": []string{"application/json"},
			"User-Agent":   []string{"fetch-synthetic-monitor"},
		})

		bodyContent, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("Falied to read endpoint request body: %v", err)
		}
		assert.Equal(t, bodyContent, []byte(`{"foo":"bar"}`))

		// delay to trigger timeout error
		if delay {
			time.Sleep(600 * time.Millisecond)
		}
	}))
	defer mock_server.Close()

	formatted_url, err := url.Parse(mock_server.URL)
	if err != nil {
		t.Errorf("Unable to parse mock server's URL")
		return
	}
	domain_name := formatted_url.Hostname()

	endpoint := Endpoint{
		Name:   "Mock Test",
		Url:    mock_server.URL,
		Method: "POST",
		Headers: map[string]string{
			"content-type": "application/json",
			"user-agent":   "fetch-synthetic-monitor",
		},
		Body: `{"foo":"bar"}`,
		Domain: &Domain{
			Name:          domain_name,
			UpCount:       0,
			TotalRequests: 0,
			Next:          nil,
		},
	}

	// make multiple requests and validate domain counts
	endpoint.GetEndpointHealth(500 * time.Millisecond)
	assert.Equal(t, endpoint.Domain.UpCount, 1)
	assert.Equal(t, endpoint.Domain.TotalRequests, 1)

	endpoint.GetEndpointHealth(500 * time.Millisecond)
	assert.Equal(t, endpoint.Domain.UpCount, 2)
	assert.Equal(t, endpoint.Domain.TotalRequests, 2)

	delay = true
	endpoint.GetEndpointHealth(500 * time.Millisecond)
	assert.Equal(t, endpoint.Domain.UpCount, 2)
	assert.Equal(t, endpoint.Domain.TotalRequests, 3)

	endpoint.GetEndpointHealth(600 * time.Millisecond)
	assert.Equal(t, endpoint.Domain.UpCount, 2)
	assert.Equal(t, endpoint.Domain.TotalRequests, 4)

	endpoint.GetEndpointHealth(610 * time.Millisecond)
	assert.Equal(t, endpoint.Domain.UpCount, 3)
	assert.Equal(t, endpoint.Domain.TotalRequests, 5)
}

func ExampleHealthCheckTargets_LogDomainHealth_noDomains() {
	var target *HealthCheckTargets = &HealthCheckTargets{
		Domains:   nil,
		Endpoints: nil,
	}

	target.LogDomainHealth()
	// Output:
	//
}

func ExampleHealthCheckTargets_LogDomainHealth_emptyDomains() {
	var target *HealthCheckTargets = &HealthCheckTargets{
		Domains:   &Domain{},
		Endpoints: nil,
	}

	target.LogDomainHealth()
	// Output:
	//
}

func ExampleHealthCheckTargets_LogDomainHealth_oneDomain() {
	var target *HealthCheckTargets = &HealthCheckTargets{
		Domains: &Domain{
			Name:          "example.com",
			UpCount:       1,
			TotalRequests: 2,
			Next:          nil,
		},
		Endpoints: nil,
	}

	target.LogDomainHealth()
	// Output:
	// example.com has 50% availability percentage
}

func ExampleHealthCheckTargets_LogDomainHealth_multipleDomains() {
	var target *HealthCheckTargets = &HealthCheckTargets{
		Domains: &Domain{
			Name:          "example.com",
			UpCount:       1,
			TotalRequests: 2,
			Next: &Domain{
				Name:          "localhost",
				UpCount:       2,
				TotalRequests: 3,
				Next:          nil,
			},
		},
		Endpoints: nil,
	}

	target.LogDomainHealth()
	// Output:
	// example.com has 50% availability percentage
	// localhost has 67% availability percentage
}

func ExampleHealthCheckTargets_LogDomainHealth_zeroTotalRequests() {
	var target *HealthCheckTargets = &HealthCheckTargets{
		Domains: &Domain{
			Name:          "example.com",
			UpCount:       0,
			TotalRequests: 0,
			Next:          nil,
		},
		Endpoints: nil,
	}

	target.LogDomainHealth()
	// Output:
	// example.com has 0% availability percentage
}
