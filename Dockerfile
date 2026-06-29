FROM golang:1.26.3

ENV TZ=Europe/Oslo

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN go build -o server ./cmd/server

EXPOSE 8135

CMD ["/app/server"]
