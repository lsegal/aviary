FROM node:25-bookworm AS web-builder

WORKDIR /src

COPY package.json pnpm-lock.yaml ./
RUN npm install -g pnpm@8.6.0
RUN pnpm install --frozen-lockfile

COPY . .
RUN pnpm build:web

FROM golang:1.27-bookworm AS go-builder

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

FROM debian:bookworm-slim AS whisper-builder

ARG TARGETARCH
ARG WHISPER_CPP_VERSION=v1.9.5
ARG WHISPER_CPP_COMMIT=d1be6fde11ac6e0407606b4e42fe72d34add8037
ARG WHISPER_MODEL_REVISION=5359861c739e955e79d9a303bcbc70fb988958b1
ARG WHISPER_MODEL_SHA256=a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002

RUN apt-get update && apt-get install -y --no-install-recommends \
  ca-certificates \
  cmake \
  curl \
  g++ \
  git \
  make \
  && rm -rf /var/lib/apt/lists/*

RUN git clone --depth 1 --branch "${WHISPER_CPP_VERSION}" https://github.com/ggml-org/whisper.cpp.git /src/whisper.cpp \
  && test "$(git -C /src/whisper.cpp rev-parse HEAD)" = "${WHISPER_CPP_COMMIT}"

# The CPU backend is built as loadable modules so whisper-cli picks the best
# one for the host at runtime instead of baking in the build machine's CPU
# features. amd64 gets every x86 variant; arm64 gets the baseline armv8-a
# backend, since bookworm's GCC cannot build ggml's SME variants.
WORKDIR /src/whisper.cpp
RUN case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
    amd64) variants=ON ;; \
    arm64) variants=OFF ;; \
    *) echo "unsupported TARGETARCH: ${TARGETARCH:-$(dpkg --print-architecture)}" >&2; exit 1 ;; \
  esac \
  && cmake -B build \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_BUILD_WITH_INSTALL_RPATH=ON \
    -DCMAKE_INSTALL_RPATH=/usr/local/lib/whisper \
    -DBUILD_SHARED_LIBS=ON \
    -DGGML_NATIVE=OFF \
    -DGGML_BACKEND_DL=ON \
    -DGGML_BACKEND_DIR=/usr/local/lib/whisper \
    -DGGML_CPU_ALL_VARIANTS="${variants}" \
    -DGGML_OPENMP=OFF \
    -DWHISPER_BUILD_TESTS=OFF \
    -DWHISPER_BUILD_SERVER=OFF \
  && cmake --build build --config Release -j "$(nproc)" --target whisper-cli \
  && mkdir -p /out/bin /out/lib/whisper /out/models \
  && install -m 0755 build/bin/whisper-cli /out/bin/whisper-cli \
  && find build -name '*.so*' \( -type f -o -type l \) -exec cp -P {} /out/lib/whisper/ \; \
  && strip --strip-unneeded /out/bin/whisper-cli \
  && find /out/lib/whisper -type f -exec strip --strip-unneeded {} +

RUN curl -fsSLo /out/models/ggml-base.en.bin \
    "https://huggingface.co/ggerganov/whisper.cpp/resolve/${WHISPER_MODEL_REVISION}/ggml-base.en.bin" \
  && echo "${WHISPER_MODEL_SHA256}  /out/models/ggml-base.en.bin" | sha256sum -c -

FROM debian:bookworm-slim

ARG TARGETARCH

RUN apt-get update && apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  ffmpeg \
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

RUN case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
    amd64) \
      curl -fsSLo /tmp/google-chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb \
      && apt-get update \
      && apt-get install -y --no-install-recommends /tmp/google-chrome.deb \
      && rm -f /tmp/google-chrome.deb \
      ;; \
    arm64) \
      apt-get update \
      && apt-get install -y --no-install-recommends chromium chromium-sandbox \
      && ln -sf /usr/bin/chromium /usr/bin/google-chrome \
      && ln -sf /usr/bin/chromium /usr/bin/google-chrome-stable \
      ;; \
    *) \
      echo "unsupported TARGETARCH: ${TARGETARCH:-$(dpkg --print-architecture)}" >&2 \
      && exit 1 \
      ;; \
  esac \
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

COPY --from=whisper-builder /out/bin/whisper-cli /usr/local/bin/whisper-cli
COPY --from=whisper-builder /out/lib/whisper/ /usr/local/lib/whisper/
COPY --from=whisper-builder /out/models/ggml-base.en.bin /usr/local/share/whisper/ggml-base.en.bin

# Aviary also finds the model in /usr/local/share/whisper, so a volume
# mounted over the data directory does not hide it; the link puts it at the
# default model path for an unmounted data directory.
RUN mkdir -p /home/bot/.config/aviary/deps/whisper \
  && ln -s /usr/local/share/whisper/ggml-base.en.bin /home/bot/.config/aviary/deps/whisper/ggml-base.en.bin \
  && chown -R bot:bot /home/bot/.config

COPY --from=go-builder /out/aviary /usr/local/bin/aviary

USER bot
ENV PATH="/home/bot/.local/bin:/home/linuxbrew/.linuxbrew/bin:${PATH}"
ENV AVIARY_SANDBOX=true
ENV AVIARY_CONFIG_SERVER_EXTERNAL_ACCESS=true
WORKDIR /home/bot
EXPOSE 1455 45289 16677

CMD ["aviary", "serve"]
