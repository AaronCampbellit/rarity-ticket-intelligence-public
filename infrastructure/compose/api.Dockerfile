FROM golang:1.26.8-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS build
WORKDIR /src
RUN apk add --no-cache tzdata
COPY go.mod go.sum ./
RUN go mod download
COPY backend ./backend
COPY LICENSE BRANDING.md THIRD_PARTY_NOTICES.md /out/legal/
COPY third-party-licenses /out/legal/third-party-licenses
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/rarity-api ./backend/cmd/rarity-api
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/rarity-admin ./backend/cmd/rarity-admin

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /out/rarity-api /rarity-api
COPY --from=build /out/rarity-admin /rarity-admin
COPY --from=build /out/legal /usr/share/licenses/rarity
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/rarity-api"]
