FROM golang:1.27.0

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod tidy

RUN go install github.com/air-verse/air@v1.61.7

RUN go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

RUN apt-get -y update && apt-get -y upgrade && apt-get install -y ffmpeg

COPY ./ ./
