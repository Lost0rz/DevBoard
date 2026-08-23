# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/devboard ./cmd/devboard

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/devboard /usr/local/bin/devboard
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/devboard", "serve", "--config", "/etc/devboard/config.yaml"]
