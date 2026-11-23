FROM golang:1.26.3 AS build

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o server ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Europe/Oslo

WORKDIR /app

COPY --from=build /app/server /app/server

EXPOSE 8135

CMD ["/app/server"]
