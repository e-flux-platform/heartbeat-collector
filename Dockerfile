FROM golang:1.24-alpine AS build-stage

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-s -extldflags "-static"' -o heartbeat-collector .

FROM build-stage AS run-test-stage
RUN go test -v ./...

FROM alpine:latest

RUN apk --no-cache add sqlite ca-certificates

WORKDIR /app

COPY --from=build-stage /app/heartbeat-collector .

EXPOSE 8080

ENTRYPOINT ["./heartbeat-collector"]