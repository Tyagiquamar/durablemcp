# Multi-stage build producing the three DurableMCP binaries plus the dashboard.
# A single image selects its role at runtime via DURABLEMCP_RUN (see
# docker/railway-entrypoint.sh): server | executor | scheduler.

FROM golang:1.26-alpine AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/durablemcp-server ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -o /bin/durablemcp-executor ./cmd/executor \
 && CGO_ENABLED=0 GOOS=linux go build -o /bin/durablemcp-scheduler ./cmd/scheduler

FROM alpine:3.21 AS runtime
RUN adduser -D -u 10001 durablemcp
COPY --from=go-builder /bin/durablemcp-server /bin/durablemcp-executor /bin/durablemcp-scheduler /
COPY docker/railway-entrypoint.sh /railway-entrypoint.sh
RUN chmod +x /railway-entrypoint.sh
USER durablemcp
ENTRYPOINT ["/railway-entrypoint.sh"]
