# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG DEVBOARD_PRODUCT_VERSION=development
ARG DEVBOARD_GIT_COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.productVersion=${DEVBOARD_PRODUCT_VERSION} -X main.gitCommit=${DEVBOARD_GIT_COMMIT}" -o /out/devboard ./cmd/devboard

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/devboard /usr/local/bin/devboard
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/devboard"]
CMD ["serve", "--config", "/etc/devboard/config.yaml"]
