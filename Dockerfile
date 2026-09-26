FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.23-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/open-ima ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mock-model ./cmd/mock-model

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=go-build /out/open-ima /app/open-ima
COPY --from=go-build /out/mock-model /app/mock-model
EXPOSE 8080
ENTRYPOINT ["/app/open-ima"]
