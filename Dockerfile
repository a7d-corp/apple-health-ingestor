# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/apple-health-ingestor ./cmd/apple-health-ingestor

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/apple-health-ingestor /apple-health-ingestor
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD ["/apple-health-ingestor", "healthcheck"]
ENTRYPOINT ["/apple-health-ingestor"]
