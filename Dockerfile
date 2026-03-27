FROM node:24-bookworm AS web-builder

WORKDIR /src

COPY package.json pnpm-lock.yaml ./
RUN corepack enable && corepack prepare pnpm@8.6.0 --activate
RUN pnpm install --frozen-lockfile

COPY . .
RUN pnpm build:web

FROM golang:1.26-bookworm AS go-builder

ARG AVIARY_VERSION=dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web-builder /src/internal/server/webdist ./internal/server/webdist

RUN CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags "-s -w -X github.com/lsegal/aviary/internal/buildinfo.Version=${AVIARY_VERSION}" \
  -o /out/aviary \
  ./cmd/aviary

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  fonts-liberation \
  git \
  libasound2 \
  libatk-bridge2.0-0 \
  libatk1.0-0 \
  libatspi2.0-0 \
  libcups2 \
  libdbus-1-3 \
  libdrm2 \
  libgbm1 \
  libgtk-3-0 \
  libnspr4 \
  libnss3 \
  libu2f-udev \
  libvulkan1 \
  libwayland-client0 \
  libxcomposite1 \
  libxdamage1 \
  libxfixes3 \
  libxkbcommon0 \
  libxrandr2 \
  sudo \
  xdg-utils \
  && rm -rf /var/lib/apt/lists/*

RUN curl -fsSLo /tmp/google-chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb \
  && apt-get update \
  && apt-get install -y --no-install-recommends /tmp/google-chrome.deb \
  && rm -f /tmp/google-chrome.deb \
  && rm -rf /var/lib/apt/lists/*

RUN useradd -m linuxbrew -d /home/linuxbrew -s /bin/bash \
  && echo "linuxbrew ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/linuxbrew \
  && chmod 0440 /etc/sudoers.d/linuxbrew

USER linuxbrew
RUN /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
RUN /home/linuxbrew/.linuxbrew/bin/brew install gogcli himalaya
RUN chmod 777 /home/linuxbrew /home/linuxbrew/.linuxbrew

USER root
RUN useradd -m bot -d /home/bot -s /bin/bash \
  && mkdir -p /home/bot/.local/bin \
  && chown -R bot:bot /home/bot/.local

COPY --from=go-builder /out/aviary /usr/local/bin/aviary

USER bot
ENV PATH="/home/bot/.config/aviary/bin:/home/bot/.local/bin:/home/linuxbrew/.linuxbrew/bin:${PATH}"
WORKDIR /home/bot

CMD ["aviary", "serve"]
