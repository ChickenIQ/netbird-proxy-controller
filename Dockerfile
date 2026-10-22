FROM golang:1.26.9-alpine AS build

RUN apk add --no-cache ca-certificates
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/netbird-proxy-controller ./cmd

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/netbird-proxy-controller /netbird-proxy-controller

USER 65532:65532

ENTRYPOINT ["/netbird-proxy-controller"]
