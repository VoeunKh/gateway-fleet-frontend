FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fleetd ./cmd/fleetd

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/fleetd /fleetd
EXPOSE 8080
ENTRYPOINT ["/fleetd"]
