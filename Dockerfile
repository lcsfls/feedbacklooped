FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /feedbacklooped .
RUN mkdir -p /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /feedbacklooped /feedbacklooped
COPY --from=build --chown=65532:65532 /data /data
ENV FL_DATA=/data FL_LISTEN=:8080
VOLUME /data
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=3s CMD ["/feedbacklooped", "healthcheck"]
ENTRYPOINT ["/feedbacklooped"]
