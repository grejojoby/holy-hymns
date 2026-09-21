# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25.5-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/holyhymns ./cmd/holyhymns

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -g 10001 app && adduser -D -u 10001 -G app app
WORKDIR /app
COPY --from=build /out/holyhymns /app/holyhymns
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/holyhymns"]
CMD ["serve"]
