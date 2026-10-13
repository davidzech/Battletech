# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal ./internal
# Pure-Go build (modernc.org/sqlite needs no cgo), so the final image can be distroless.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /battletech .
# Empty data dir owned by the runtime user, so a named volume mounted here is writable.
RUN mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /battletech /app/battletech
COPY --from=build --chown=nonroot:nonroot /data /data
COPY campaigns /app/campaigns
VOLUME /data
EXPOSE 8080
# Campaign Markdown is read from /app/campaigns on every request; playthroughs live in /data.
ENTRYPOINT ["/app/battletech", "-addr", ":8080", "-content", "/app/campaigns", "-db", "/data/playthroughs.db"]
