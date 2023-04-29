# Let's Check Health (checkhealth)
LetsCheckHealth is a simple CLI program that takes a defined endpoint configuration file as an intput and uses it to run HTTP client requests every 15 second. An endpoint is then labeled as UP if the endpoint returns a status code between 200 and 299 and the response latency is less than 500ms. Otherwise, the node is labeled as down.

Using the endpoint status, cumulative domain availability is printed to the console every 15 seconds over the lifetime of the process. A domain is the fully qualified domain name (FQDN) of an endpoint, where it's possible to have multiple endpoints. Also note, cumulative availability data does not persist across executions of the program.

## Installation, Build, and Run
### Requirements
To build and run, you will need to have the following installed:
- Go (version 1.16 or later)
- Git

### Installation
1. Clone the repository to your local machine:
```
$ git clone https://github.com/gpjservais/LetsCheckHealth.git
```

2. Navigate to the project directory:
```
$ cd LetsCheckHealth
```

3. Install the required dependencies:
```
$ go mod download
```

### Build
To build the program, run the following command within the project directory:
```
$ go build
```

This will create an executable file named `checkhealth` (or `checkhealth.exe` on Windows) in the project directory.

### Run
To run the program, run the following command in the project directory, replacing `<file>` with the path of your YAML endpoint configuration file:
```
(MacOS/Linux/Unix)
$ ./checkhealth <file>

(Windows)
$ checkhealth.exe <file>
```

## Configuration
### Required Arguments:
`file`
- file should be the relative or absolute path to an endpoint yaml configuration file.

### Configuration File:
The configuration file defines a list of endpoints to query in YAML. It has the following schema:

`name` (string, required)
- A free-text description of the endpoint.

`url` (string, required)
- The URL of the HTTP endpoint. It is assumed to be valid.

`method` (string, optional)
- The HTTP method to use. If not provided, the GET method is used. It is assumed a valid method is provided.

`headers` (dictionary, optional)
- The HTTP headers to add or modify the default HTTP client request. It is assumed that these are valid and single valued.

`body` (string, optional)
- A JSON-encoded string to be sent in the request. If not provided, no body is sent in the request.

Example:
```yaml
- name: fetch.com some post endpoint
  url: https://fetch.com/some/post/endpoint
  method: POST
  headers:
    content-type: application/json
    user-agent: fetch-synthetic-monitor
  body: '{"foo":"bar"}'
```

## Dependencies (Not from the Go Standard Library)
[github.com/go-yaml/yaml](https://github.com/go-yaml/yaml)
- Used to parse out YAML configuration.

[github.com/stretchr/testify/assert](https://github.com/go-playground/assert)
- Used to assist with testing.

You can install these modules by running the following command:
```
go get github.com/go-yaml/yaml
go get github.com/stretchr/testify/assert
```

## License
[MIT](LICENSE)
