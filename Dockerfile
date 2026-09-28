# Build the admin UI first, then compile the Go service into the same image.
FROM node:22-alpine AS frontend
WORKDIR /src
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ENV http_proxy=${HTTP_PROXY} \
    https_proxy=${HTTPS_PROXY} \
    all_proxy=${ALL_PROXY} \
    HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    ALL_PROXY=${ALL_PROXY}
RUN corepack enable
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM golang:1.23-alpine AS backend
ARG TARGETARCH
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ENV http_proxy=${HTTP_PROXY} \
    https_proxy=${HTTPS_PROXY} \
    all_proxy=${ALL_PROXY} \
    HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    ALL_PROXY=${ALL_PROXY}
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags='-s -w' -o /out/imagehub ./cmd/server

FROM alpine:3.21
RUN addgroup -S imagehub && adduser -S -G imagehub imagehub \
    && apk add --no-cache ca-certificates tzdata ffmpeg
WORKDIR /app
COPY --from=backend /out/imagehub /app/imagehub
COPY --from=frontend /src/dist /app/web/dist
RUN mkdir -p /app/uploads && chown -R imagehub:imagehub /app
USER imagehub
ENV ADDR=:8080 WEB_DIR=/app/web/dist STORAGE_DIR=/app/uploads
EXPOSE 8080
ENTRYPOINT ["/app/imagehub"]
