/*
CheckHealth validates if HTTP endpoints are healthy every 15 seconds.

DESCRIPTION:

	CheckHealth is a simple CLI program that takes a defined endpoint configuration file as an
	intput and uses it to run HTTP client requests every 15 second. An endpoint is then labeled
	as UP if the endpoint returns a status code between 200 and 299 and the response latency is
	less than 500ms. Otherwise, the node is labeled as down.

	Using the endpoint status, cumulative domain availability is printed to the console every 15
	seconds over the process lifetime. A domain is the fully qualified domain name (FQDN) of an
	endpoint, where it's possible to have multiple endpoints. Also note, cumulative availability
	data does not persist across executions of the program.

USAGE:

	(MacOS/Linux) ./checkhealth file
	(Windows)     checkhealth.exe file

REQUIRED ARGUMENT:

	file
		file should be the relative or absolute path to an endpoint yaml configuration file.

CONFIGURATION FILE:

	The configuration file defines a list of endpoints to query in YAML. It has the following
	schema:
		name (string, required)
			A free-text description of the endpoint.

		url (string, required)
			The URL of the HTTP endpoint. It is assumed to be valid.

		method (string, optional)
			The HTTP method to use. If not provided, the GET method is used. It is assumed a
			valid method is provided.

		headers (dictionary, optional)
			The HTTP headers to add or modify the default HTTP client request. It is assumed
			that these are valid.

		body (string, optional)
			A JSON-encoded string to be sent in the request. If not provided, no body is sent
			in the request.

	Example:
		- name: fetch.com some post endpoint
		  url: https://fetch.com/some/post/endpoint
		  method: POST
		  headers:
		    content-type: application/json
		    user-agent: fetch-synthetic-monitor
		  body: '{"foo":"bar"}'

EXIT STATUS:

	CheckHealth will exit early with a non-zero exit if any configuration steps fail.

EXAMPLE USAGE:

	$ ./checkhealth config.yaml

	Console Output:
		fetch.com has 0% availability percentage
		www.fetchrewards.com has 100% availability percentage
*/
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v2"
)

// Endpoints is a slice of Endpoint used to unmarshal endpoint configuration for a provided
// YAML file.
type Endpoint struct {
	Name    string            `yaml:"name"`
	Url     string            `yaml:"url"`
	Method  string            `yaml:"method,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Body    string            `yaml:"body,omitempty"`

	Domain *Domain
}

type Endpoints []Endpoint

// The domain object is used to maintain the HTTP request details for a single domain's
// availability. It is designed as to be a linked list to be used with HealthCheckTargets.
type Domain struct {
	Name          string
	UpCount       int
	TotalRequests int
	Next          *Domain
}

// HealthCheckTargets is the primary object for performing healthchecks. It contains a pointer to
// the head of a linked list for both the Domain and a pointer to the Endpoints object.
type HealthCheckTargets struct {
	Domains   *Domain
	Endpoints *Endpoints
}

// EndpointUp and EndpointDown are boolean aliases used to with UpdateDomainStats to update whether
// an endpoint in a domain is up or down.
const (
	EndpointUp   bool = true
	EndpointDown bool = false
)

// Usage provides help text if an error is encountered while running GetConfig. Upon failure, the
// usage text will be displayed along with the error.
const Usage string = `
USAGE: (MacOS/Linux) checkhealth file
       (Windows)     checkhealth.exe file

REQUIRED ARGUMENT:

	file
		file should be the relative or absolute path to an endpoint yaml configuration file.
`

// UsageConfig provides help text for the format required for the configuration file. It is
// returned when the provided file exists, but is not provided in the correct format.
const UsageConfig string = `
CONFIGURATION FILE:

	The configuration file defines a list of endpoints to query in YAML. It has the following
	schema:
		name (string, required)
			A free-text description of the endpoint.

		url (string, required)
			The URL of the HTTP endpoint. It is assumed to be valid.

		method (string, optional)
			The HTTP method to use. If not provided, the GET method is used. It is assumed a
			valid method is provided.

		headers (dictionary, optional)
			The HTTP headers to add or modify the default HTTP client request. It is assumed
			that these are valid.

		body (string, optional)
			A JSON-encoded string to be sent in the request. If not provided, no body is sent
			in the request.

	Example:
		- name: fetch.com some post endpoint
		  url: https://fetch.com/some/post/endpoint
		  method: POST
		  headers:
		    content-type: application/json
		    user-agent: fetch-synthetic-monitor
		  body: '{"foo":"bar"}'
`

// GetConfig checks for command line arguments passed when executing the program and validates that
// a valid endpoint YAML configuration file was provided. If invalid, the function will return
// early with an error containing usage details for the CheckHealth program.
//
// Note: It is assumed that the full configuration file is small enough to be safely loaded entirely
// in memory.
func GetConfig() (Endpoints, error) {
	// read CLI arguments to get config file
	if len(os.Args) != 2 {
		err := fmt.Errorf("checkhealth requires a single argument for file.\n%s", Usage)
		return nil, err
	}

	// verify that the file exists
	file := os.Args[1]
	if _, err := os.Stat(file); err != nil {
		err = fmt.Errorf("failed to stat file: %v\n%s", err, Usage)
		return nil, err
	}

	// load entire config file into memory
	loaded_config, err := os.ReadFile(file)
	if err != nil {
		err = fmt.Errorf("failed to read file: %v\n%s", err, Usage)
		return nil, err
	}

	// unmarshal YAML into EndpointConfig
	var endpoint_objects Endpoints
	err = yaml.Unmarshal(loaded_config, &endpoint_objects)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal config YAML: %v\n%s\n%s", err, Usage, UsageConfig)
		return nil, err
	}

	// return EndpointConfig
	return endpoint_objects, nil
}

// CreateRequest wraps around http.Request to create a new HTTP request.
//
// The function takes an HTTP method, URL, JSON-formatted body, and headers. It returns a pointer to an
// HTTP request and an error. An error is returned if it fails to create a new request.
//
// If a method isn't provided, it defaults to the GET method.
// If a body isn't provided, a nil is passed when creating the new request.
// If headers are provided, they will be added and override any default header values.
//
// Note: Headers are assumed to be single valued.
func CreateRequest(ctx context.Context, method string, raw_url string, body string, headers map[string]string) (*http.Request, error) {
	// Body to io.Reader interface
	var body_reader io.Reader = nil

	if body != "" {
		body_reader = bytes.NewReader([]byte(body))
	}

	if method == "" {
		method = "GET"
	}

	// creates the HTTP request
	request, err := http.NewRequestWithContext(ctx, method, raw_url, body_reader)
	if err != nil {
		return nil, err
	}

	// Add any required headers
	for field, value := range headers {
		request.Header.Set(field, value)
	}

	return request, nil
}

// UpdateDomainStats is a method for a domain to update availability statistics.
//
// The method takes a boolean input denoting whether a endpoint was recorded as up in the domain.
// If it was, then the domain's up count will increment by 1.
// Calling UpdateDomainStats will always update a domain's the total number of requests by 1.
//
// Returns immediately if the domain pointer passed is not nil.
func (domain *Domain) UpdateDomainStats(is_up bool) {
	if domain == nil {
		return
	}

	if is_up {
		domain.UpCount += 1
	}

	domain.TotalRequests += 1
}

// GetEndpointHealth is a method that has a provided HTTP client run an endpoint's request and
// determine the endpoint's health. If an error is encountered while performing the request or if
// the status code of the server response is not between 200 and 299, the endpoint is considered
// "down". Otherwise, it will be considered up.
//
// Context is used to cause response times longer than max_latency to trigger a timeout timeout and
// to cancel the request, resulting in the endpoint getting marked as "down".
//
// The status of the endpoint is fed to the endpoint's associated domain through UpdateDomainStats,
// which is used to keep track of the health of the domain.
func (endpoint *Endpoint) GetEndpointHealth(max_latency time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), max_latency)
	defer cancel()

	// forcing creating request to be fatal as it's a configuration issue
	// this should be validated in CreateNewTargets()
	request, err := CreateRequest(ctx, endpoint.Method, endpoint.Url, endpoint.Body, endpoint.Headers)
	if err != nil {
		log.Fatalf("ERROR: Failed to create HTTP Request: %v", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		endpoint.Domain.UpdateDomainStats(EndpointDown)
		return
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		endpoint.Domain.UpdateDomainStats(EndpointDown)

		// added to ensure that we close our connection properly
		io.ReadAll(response.Body)
		return
	}

	// added to ensure that we close our connection properly
	io.ReadAll(response.Body)

	endpoint.Domain.UpdateDomainStats(EndpointUp)
}

// CreateNewTargets is a function that takes an endpoint configuration object and returns a new
// HealthCheckTargets object that contains a domains linked list and a pointer to the endpoints.
//
// Any failures to generate a domain or endpoint object will considered critical and result in the
// method exiting early with an error.
func (endpoints *Endpoints) CreateNewTargets() (HealthCheckTargets, error) {
	// creates a new HealthCheckTarget Object
	var target HealthCheckTargets = HealthCheckTargets{
		Domains:   nil,
		Endpoints: endpoints,
	}

	// create endpoints for each configuration object
	for i := 0; i < len(*endpoints); i++ {
		// validate successful creation of HTTP requests
		_, err := CreateRequest(
			context.Background(),
			(*endpoints)[i].Method,
			(*endpoints)[i].Url,
			(*endpoints)[i].Body,
			(*endpoints)[i].Headers,
		)
		if err != nil {
			err = fmt.Errorf("failed to create new HTTP request: %v", err)
			return HealthCheckTargets{}, err
		}

		// get pointer to domain associated with endpoint.
		domain_pointer, err := target.GetDomainPointer((*endpoints)[i].Url)
		if err != nil {
			err = fmt.Errorf("failed to get domain: %v", err)
			return HealthCheckTargets{}, err
		}

		// create the new endpoint
		(*endpoints)[i].Domain = domain_pointer
	}

	return target, nil
}

// GetDomainPointer is a method for HealthCheckTargets that returns a pointer to a domain for a
// provided URL. GetDomainPointer will create a new domain and add it to the end of
// HealthCheckTargets' linked list if it doesn't already exist.
//
// If any errors are encountered while attempting to parse the provided URL string,
// GetDomainPointer will fail and an error will be returned.
//
// Note: a domain is the fully qualified domain name (FQDN) of the provided URL. So "www.google.com" and
// "google.com" would resolve as separate domains.
func (target *HealthCheckTargets) GetDomainPointer(raw_url string) (*Domain, error) {
	// return with an error if target is a null pointer
	if target == nil {
		return nil, fmt.Errorf("failed to create domain pointer, *HealthCheckTargets is nil")
	}
	// return with an error if an empty string is provided
	if raw_url == "" {
		return nil, fmt.Errorf("failed to create domain pointer, provided URL was an empty string")
	}

	// get domain name from URL
	current_url, err := url.Parse(raw_url)
	if err != nil {
		return nil, err
	}
	domain_name := current_url.Hostname()

	var current_domain *Domain = target.Domains
	var previous_domain *Domain = nil

	// handle case where domain already exists
	for current_domain != nil {
		if domain_name == current_domain.Name {
			return current_domain, nil
		}

		previous_domain = current_domain
		current_domain = current_domain.Next
	}

	// handle case where domain doesn't exist
	new_domain := &Domain{
		Name:          domain_name,
		UpCount:       0,
		TotalRequests: 0,
		Next:          nil,
	}

	if target.Domains == nil {
		target.Domains = new_domain
	} else {
		previous_domain.Next = new_domain
	}

	return new_domain, nil
}

// RunCheckHealth is a method for HealthCheckTargets that will run until the process is terminated.
// Every 15 seconds RunCheckHealth will execute client request to the endpoints defined in the
// HealthCheckTargets' Endpoints slice. Requests are executed in series. Once all endpoint health
// checks are complete, a call to LogDomainHealth() is made to log the output.
func (target *HealthCheckTargets) RunCheckHealth() {
	throttle := time.Tick(15 * time.Second)

	for {
		for _, endpoint := range *target.Endpoints {
			// get the status of the endpoint and update domains counts
			// defines max latency as 500ms
			endpoint.GetEndpointHealth(500 * time.Millisecond)
		}

		// call logger to log output
		target.LogDomainHealth()

		// Trigger new checks every 15 seconds
		<-throttle
	}
}

// LogDomainHealth is a method for HealthCheckTargets that iterates through the Domains linked list.
// It computes the cumulative domain availability of each domain over the lifetime of the process,
// rounding to the nearest whole number. Each domain's availabilty is printed to the console.
func (target *HealthCheckTargets) LogDomainHealth() {
	domain := target.Domains

	for domain != nil {
		// An empty domains should not exist. If they do, don't report on them.
		if domain.Name == "" {
			domain = domain.Next
			continue
		}

		// If no requests have been run for a domain, report 0% availability.
		var availability int = 0

		if domain.TotalRequests != 0 {
			availability = int(math.Round(100 * float64(domain.UpCount) / float64(domain.TotalRequests)))
		}

		fmt.Printf("%s has %d%% availability percentage\n", domain.Name, availability)

		domain = domain.Next
	}
}

// Main entry point when the program is executed directly. It will run GetConfig to get the
// endpoint configuration from a provided file. Then, it'll create HealthCheckTargets object based
// on the configuration and use RunCheckHealth until the program is exited by terminating the
// program.
func main() {
	endpoint_config, err := GetConfig()
	if err != nil {
		log.Fatalf("ERROR: %v\n", err)
	}

	targets, err := endpoint_config.CreateNewTargets()
	if err != nil {
		log.Fatalf("ERROR: %v\n", err)
	}

	targets.RunCheckHealth()
}
