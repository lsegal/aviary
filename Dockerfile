FROM debian:latest AS base
# Chrome dependency Instalation
RUN apt-get update && apt-get install -y \
  curl \
  fonts-liberation \
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
  libwayland-client0 \
  libxcomposite1 \
  libxdamage1 \
  libxfixes3 \
  libxkbcommon0 \
  libxrandr2 \
  xdg-utils \
  libu2f-udev \
  libvulkan1
# Chrome installation
RUN curl -LO  https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
RUN apt-get install -y ./google-chrome-stable_current_amd64.deb
RUN rm google-chrome-stable_current_amd64.deb

# Install Homebrew
RUN apt install -y sudo git
RUN useradd -m linuxbrew -d /home/linuxbrew -s /bin/bash
RUN echo "linuxbrew ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/linuxbrew && chmod 0440 /etc/sudoers.d/linuxbrew
USER linuxbrew
RUN /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
RUN /home/linuxbrew/.linuxbrew/bin/brew install gogcli himalaya
RUN chmod 777 /home/linuxbrew/ && chmod 777 /home/linuxbrew/.linuxbrew

# Create a default user
USER root
RUN useradd -m bot -d /home/bot -s /bin/bash
RUN mkdir -p /home/bot/.local/bin && chown -R bot:bot /home/bot/.local
USER bot
ENV PATH="/home/bot/.config/aviary/bin:/home/bot/.local/bin:/home/linuxbrew/.linuxbrew/bin:${PATH}"
WORKDIR /home/bot

# Install aviary release
RUN curl -fsSL https://aviary.bot/install.sh | sh

CMD [ "aviary", "serve" ]
