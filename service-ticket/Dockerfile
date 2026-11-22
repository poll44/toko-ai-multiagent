# Build context = ROOT repo (module Go tunggal):  docker build -f service-ticket/Dockerfile .
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY service-ticket ./service-ticket
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/service ./service-ticket

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata  && adduser -D -u 10001 app && mkdir -p /app/data && chown app /app/data
WORKDIR /app
COPY --from=build /out/service /app/service
USER app
ENV TICKET_PORT=4003
EXPOSE 4003
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5   CMD wget -qO- http://127.0.0.1:${TICKET_PORT}/health >/dev/null || exit 1
ENTRYPOINT ["/app/service"]
