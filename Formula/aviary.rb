# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.3'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.3/aviary_v1.0.3_darwin_arm64.tar.gz'
      sha256 '334b5a4d0b38f94bd7d4f4c42c948f253276c2f0e12f551ecb6b2ef648e19343'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.3/aviary_v1.0.3_darwin_amd64.tar.gz'
      sha256 '4846d9eec6df0683b9acb8c78b85dca104e9908a35819e208e84206fe55cefc0'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.3/aviary_v1.0.3_linux_arm64.tar.gz'
      sha256 '5fa612cbd91585dc3f975d9e716c200e3ace3fe3ed210850385120d1a08b3c98'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.3/aviary_v1.0.3_linux_amd64.tar.gz'
      sha256 'be513fe7048203c552a9ccb3e7d79de0f83828747e74c3ccb8045bdfc76300e8'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
