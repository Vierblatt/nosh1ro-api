FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o server ./cmd/api/

FROM scratch
COPY --from=build /app/server /server
COPY encryption.json go-plan-encryption.json /
EXPOSE 8080
ENTRYPOINT ["/server"]
