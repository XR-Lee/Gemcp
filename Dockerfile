ARG NODE_IMAGE=node:22-alpine
ARG GO_IMAGE=golang:1.26.5-alpine
ARG RUNTIME_IMAGE=alpine:3.23

FROM ${NODE_IMAGE} AS frontend-builder
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --include=dev
COPY frontend/ ./
RUN npm run build

FROM ${GO_IMAGE} AS backend-builder
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf internal/web/dist && mkdir -p internal/web/dist
COPY --from=frontend-builder /src/frontend/dist/ ./internal/web/dist/
RUN CGO_ENABLED=0 go build -tags webembed -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
    -o /out/gemcp ./cmd/gemcp

FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates tzdata git openssh-client && \
    addgroup -S -g 10001 gemcp && adduser -S -D -H -u 10001 -G gemcp gemcp
WORKDIR /app
COPY --from=backend-builder /out/gemcp /usr/local/bin/gemcp
USER gemcp
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gemcp"]
CMD ["serve"]
