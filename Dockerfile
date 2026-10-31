# syntax=docker/dockerfile:1
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
# Optional session CA mount supports managed environments without storing it.
RUN --mount=type=secret,id=proxy_ca \
    if [ -f /run/secrets/proxy_ca ]; then SSL_CERT_FILE=/run/secrets/proxy_ca go mod download; else go mod download; fi
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN --network=none CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/api ./cmd/api && \
	CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/email-worker ./cmd/email-worker && \
	CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/media-worker ./cmd/media-worker && \
 CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/notification-worker ./cmd/notification-worker

FROM gcr.io/distroless/static-debian12:nonroot AS api
COPY --from=build /out/api /app/api
COPY --from=build /out/email-worker /app/email-worker
COPY --from=build /out/media-worker /app/media-worker
COPY --from=build /out/notification-worker /app/notification-worker
COPY deploy/city4christ-media-worker.service /app/city4christ-media-worker.service
USER 65532:65532
EXPOSE 10000
CMD ["/app/api"]
