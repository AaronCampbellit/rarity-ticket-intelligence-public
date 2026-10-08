FROM node:22-alpine@sha256:c610fcdfb1d5b4740dd70c284ed3cb16bb857e0f7166196e36a5501df7a3aa32 AS build
ARG RARITY_BUILD_REVISION=unknown
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend ./
COPY LICENSE BRANDING.md THIRD_PARTY_NOTICES.md /src/
COPY third-party-licenses /src/third-party-licenses
RUN case "$RARITY_BUILD_REVISION" in *[!A-Za-z0-9._-]* | '') exit 1 ;; esac \
    && npm run build \
    && sed -i "s/__RARITY_BUILD_REVISION__/$RARITY_BUILD_REVISION/g" dist/index.html

FROM golang:1.26.8-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS server
WORKDIR /src
COPY go.mod go.sum ./
COPY infrastructure/compose/frontend-server ./frontend-server
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/frontend-server ./frontend-server

FROM scratch
COPY --from=server /out/frontend-server /frontend-server
COPY --from=build --chown=65532:65532 /src/frontend/dist /usr/share/rarity
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/frontend-server"]
