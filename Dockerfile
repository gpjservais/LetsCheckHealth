FROM golang:1.20.1-alpine

WORKDIR /app

COPY go.mod .
COPY go.sum .
COPY config.yaml .

RUN go mod download

COPY *.go .

RUN go build

#CMD [ "./checkhealth config.yaml" ]
