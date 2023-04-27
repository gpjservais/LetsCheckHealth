build:
	go build .

test:
	go test -v

clean:
	rm checkhealth

run-example:
	go run . config.yaml