# ──────────────────────────────────────────────────
# Homeland Dashboard — Multi-stage Dockerfile
# ──────────────────────────────────────────────────

# Stage 1: Build Tailwind CSS
FROM node:20-alpine AS css-builder
WORKDIR /build
COPY package.json tailwind.config.js ./
RUN npm install
COPY web/static/css/app.css web/static/css/
COPY web/templates/ web/templates/
RUN npx tailwindcss -i web/static/css/app.css -o web/static/css/tailwind.css --minify

# Stage 2: Build Go binary
FROM golang:1.25-alpine AS go-builder
RUN apk add --no-cache git
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o homeland ./cmd/homeland/

# Stage 3: Minimal runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -h /app homeland
WORKDIR /app

# Copy Go binary
COPY --from=go-builder /build/homeland .

# Copy frontend assets
COPY --from=css-builder /build/web/static/css/tailwind.css web/static/css/
COPY web/templates/ web/templates/
COPY web/static/js/ web/static/js/
COPY web/static/css/app.css web/static/css/

# Copy default config
COPY configs/ configs/

# Create icon cache directory
RUN mkdir -p web/static/icons && chown -R homeland:homeland /app

USER homeland

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:3000/ || exit 1

ENTRYPOINT ["./homeland"]
